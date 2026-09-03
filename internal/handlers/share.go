package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PalashSinha14/evernote/internal/config"
	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/PalashSinha14/evernote/internal/models"
	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/PalashSinha14/evernote/internal/utils"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// sharePasswordHeader carries a share's password on the public read.
//
// A GET request has no conventional place for a secret: a query parameter
// lands in server access logs, browser history and Referer headers, which a
// password never should. A header avoids all three, at the cost of the link
// no longer being openable by pasting it into a browser's address bar — an
// acceptable trade for a backend-only API with no address-bar client yet.
const sharePasswordHeader = "X-Share-Password"

// shareTokenBytes is the size of a minted share token before hex encoding.
// Matches the size used for a token's jti in internal/utils/token.go.
const shareTokenBytes = 16

// maxTokenMintAttempts bounds the retry loop against a token collision. With
// 16 random bytes a collision is not expected to happen in this service's
// lifetime; the loop exists so the failure path is still handled correctly if
// it ever does.
const maxTokenMintAttempts = 3

// NoteLookup is the slice of the notes repository the share handler needs:
// just enough to confirm a note exists, is not deleted, and who owns it.
type NoteLookup interface {
	GetByID(ctx context.Context, id string) (*models.Note, error)
}

// ShareStore is the slice of the shares repository this handler needs.
type ShareStore interface {
	Create(ctx context.Context, s *models.Share) error
	FindByToken(ctx context.Context, token string) (*models.Share, error)
	IncrementClicks(ctx context.Context, id bson.ObjectID) error
}

// ShareHandler serves share creation and the public share read.
type ShareHandler struct {
	notes  NoteLookup
	shares ShareStore
	cfg    *config.Config
}

// NewShareHandler wires a ShareHandler to its dependencies.
func NewShareHandler(notes NoteLookup, shares ShareStore, cfg *config.Config) *ShareHandler {
	return &ShareHandler{notes: notes, shares: shares, cfg: cfg}
}

// Create handles POST /api/v1/notes/:id/share.
func (h *ShareHandler) Create(c *gin.Context) {
	ownerID, ok := h.callerID(c)
	if !ok {
		return
	}

	note, err := h.notes.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Note not found", nil)
			return
		}
		respondInternal(c)
		return
	}
	if note.OwnerID != ownerID {
		respondError(c, http.StatusForbidden, schemas.CodeForbidden,
			"You do not have access to this note", nil)
		return
	}

	var req schemas.CreateShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// An entirely empty body is valid here — both fields are optional —
		// so only a body with content that fails validation, or malformed
		// JSON, reaches this branch.
		if !isEmptyBodyErr(err) {
			respondBindError(c, err)
			return
		}
	}

	share := &models.Share{NoteID: note.ID, OwnerID: ownerID}

	if req.Password != nil {
		hash, err := utils.HashPassword(*req.Password, h.cfg.BcryptCost)
		if err != nil {
			respondInternal(c)
			return
		}
		share.PasswordHash = hash
	}

	if req.ExpiresIn != nil {
		exp := time.Now().UTC().Add(time.Duration(*req.ExpiresIn) * time.Second)
		share.ExpiresAt = &exp
	}

	if err := h.mintUniqueShare(c.Request.Context(), share); err != nil {
		respondInternal(c)
		return
	}

	c.JSON(http.StatusCreated, schemas.CreateShareResponse{
		ShareID:   share.ID.Hex(),
		URL:       h.shareURL(share.Token),
		ExpiresAt: share.ExpiresAt,
	})
}

// Access handles GET /s/:share_token. It is the one route in this service
// that serves a resource to a caller who has not authenticated at all — see
// the deep dive on the three audiences of a note in the explain document.
func (h *ShareHandler) Access(c *gin.Context) {
	share, err := h.shares.FindByToken(c.Request.Context(), c.Param("token"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Share link not found", nil)
			return
		}
		respondInternal(c)
		return
	}

	if share.HasExpired(time.Now().UTC()) {
		respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Share link has expired", nil)
		return
	}

	if share.RequiresPassword() {
		supplied := c.GetHeader(sharePasswordHeader)
		if supplied == "" || !utils.CheckPassword(share.PasswordHash, supplied) {
			respondError(c, http.StatusUnauthorized, schemas.CodeUnauthorized,
				"This share link requires a password", nil)
			return
		}
	}

	// The note lookup filters is_deleted, so a note the owner has since
	// deleted stops resolving here even though the share document itself
	// still exists — soft delete on the note cascades to every share pointing
	// at it with no extra bookkeeping.
	note, err := h.notes.GetByID(c.Request.Context(), share.NoteID.Hex())
	if err != nil {
		respondError(c, http.StatusNotFound, schemas.CodeNotFound, "This note is no longer available", nil)
		return
	}

	// Counted after every check has passed, so only a genuine read of the note
	// increments it — a wrong password does not inflate the count. A failure
	// here must never fail the response: whether the counter update succeeded
	// has nothing to do with whether the visitor is entitled to read the note.
	if err := h.shares.IncrementClicks(c.Request.Context(), share.ID); err != nil {
		fmt.Printf("share click count not recorded for %s: %v\n", share.ID.Hex(), err)
	}

	tags := note.Tags
	if tags == nil {
		tags = []string{}
	}
	c.JSON(http.StatusOK, schemas.SharedNoteResponse{
		Title:     note.Title,
		Body:      note.Body,
		Tags:      tags,
		CreatedAt: note.CreatedAt,
		UpdatedAt: note.UpdatedAt,
	})
}

// mintUniqueShare generates a token, attaches it to share, and inserts it,
// retrying with a fresh token if it collides with one already in use.
func (h *ShareHandler) mintUniqueShare(ctx context.Context, share *models.Share) error {
	var err error
	for i := 0; i < maxTokenMintAttempts; i++ {
		token, tokErr := utils.RandomToken(shareTokenBytes)
		if tokErr != nil {
			return tokErr
		}
		share.Token = token

		err = h.shares.Create(ctx, share)
		if err == nil || !errors.Is(err, db.ErrDuplicateToken) {
			return err
		}
	}
	return err
}

// shareURL builds the public URL for a token from the configured base URL.
func (h *ShareHandler) shareURL(token string) string {
	return fmt.Sprintf("%s/s/%s", strings.TrimRight(h.cfg.AppBaseURL, "/"), token)
}

// callerID returns the authenticated caller's ObjectID, writing the response
// itself and returning false when that is not possible.
func (h *ShareHandler) callerID(c *gin.Context) (bson.ObjectID, bool) {
	raw, ok := middleware.UserID(c)
	if !ok {
		respondInternal(c)
		return bson.ObjectID{}, false
	}
	oid, err := bson.ObjectIDFromHex(raw)
	if err != nil {
		respondInternal(c)
		return bson.ObjectID{}, false
	}
	return oid, true
}

// isEmptyBodyErr reports whether a JSON bind failure was caused by a request
// carrying no body at all, which is valid for POST /notes/:id/share since
// every field in CreateShareRequest is optional.
func isEmptyBodyErr(err error) bool {
	return err.Error() == "EOF"
}
