package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/PalashSinha14/evernote/internal/models"
	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/PalashSinha14/evernote/internal/utils"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// NoteStore is the slice of the notes repository this handler needs.
//
// Depending on an interface rather than *db.NoteRepo keeps the HTTP layer
// testable without MongoDB: the access-control rules below are the most
// security-sensitive code in the project, and they can be exercised against a
// fake store in memory.
type NoteStore interface {
	Create(ctx context.Context, n *models.Note) error
	List(ctx context.Context, f db.NoteFilter) ([]models.Note, int64, error)
	DistinctTags(ctx context.Context, ownerID bson.ObjectID) ([]string, error)
	GetByID(ctx context.Context, id string) (*models.Note, error)
	Update(ctx context.Context, id string, ownerID bson.ObjectID, upd db.NoteUpdate) (*models.Note, error)
	SoftDelete(ctx context.Context, id string, ownerID bson.ObjectID) error
}

// NoteHandler serves the note CRUD endpoints.
type NoteHandler struct {
	notes NoteStore
}

// NewNoteHandler wires a NoteHandler to its store.
func NewNoteHandler(notes NoteStore) *NoteHandler {
	return &NoteHandler{notes: notes}
}

// Create handles POST /api/v1/notes.
func (h *NoteHandler) Create(c *gin.Context) {
	ownerID, ok := h.callerID(c)
	if !ok {
		return
	}

	var req schemas.CreateNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	note := &models.Note{
		OwnerID:  ownerID,
		Title:    req.Title,
		Body:     req.Body,
		Tags:     utils.NormaliseTags(req.Tags),
		IsPublic: req.IsPublic,
	}
	if err := h.notes.Create(c.Request.Context(), note); err != nil {
		respondInternal(c)
		return
	}

	c.JSON(http.StatusCreated, schemas.CreateNoteResponse{
		ID:        note.ID.Hex(),
		CreatedAt: note.CreatedAt,
	})
}

// List handles GET /api/v1/notes.
//
// api_spec.md gives five optional query parameters — q, tag, page, limit and
// sort — and a { data, meta } response envelope.
func (h *NoteHandler) List(c *gin.Context) {
	ownerID, ok := h.callerID(c)
	if !ok {
		return
	}

	var q schemas.ListNotesQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respondBindError(c, err)
		return
	}

	sortField, sortDesc, err := parseSort(q.Sort)
	if err != nil {
		respondError(c, http.StatusBadRequest, schemas.CodeInvalidInput, err.Error(), nil)
		return
	}

	filter := db.NoteFilter{
		OwnerID:   ownerID,
		Query:     strings.TrimSpace(q.Q),
		Tag:       utils.NormaliseTag(q.Tag),
		Page:      orDefault(q.Page, schemas.DefaultPage),
		Limit:     orDefault(q.Limit, schemas.DefaultLimit),
		SortField: sortField,
		SortDesc:  sortDesc,
	}

	notes, total, err := h.notes.List(c.Request.Context(), filter)
	if err != nil {
		respondInternal(c)
		return
	}

	// An empty page is serialised as [] rather than null, so a client can
	// iterate the result without a nil check.
	data := make([]schemas.NoteResponse, 0, len(notes))
	for i := range notes {
		data = append(data, toNoteResponse(&notes[i]))
	}

	c.JSON(http.StatusOK, schemas.ListNotesResponse{
		Data: data,
		Meta: schemas.PageMeta{Page: filter.Page, Limit: filter.Limit, Total: total},
	})
}

// Tags handles GET /api/v1/tags.
func (h *NoteHandler) Tags(c *gin.Context) {
	ownerID, ok := h.callerID(c)
	if !ok {
		return
	}

	tags, err := h.notes.DistinctTags(c.Request.Context(), ownerID)
	if err != nil {
		respondInternal(c)
		return
	}

	c.JSON(http.StatusOK, schemas.TagsResponse{Data: tags})
}

// parseSort turns the sort query parameter into a field and a direction.
//
// api_spec.md defines the value as a field name, optionally prefixed with "-"
// for descending. The field is checked against an explicit allowlist rather
// than passed through, because it ends up as a key in a MongoDB sort document:
// an arbitrary value would let a caller sort by any field in the collection,
// including ones the API does not expose, and would sort without an index.
//
// The default is newest-updated-first, which is the order the compound
// owner_id + updated_at index already stores.
func parseSort(raw string) (field string, desc bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return schemas.SortUpdatedAt, true, nil
	}

	desc = strings.HasPrefix(raw, "-")
	field = strings.TrimPrefix(raw, "-")

	switch field {
	case schemas.SortCreatedAt, schemas.SortUpdatedAt:
		return field, desc, nil
	default:
		return "", false, fmt.Errorf("sort must be %s or %s, optionally prefixed with -",
			schemas.SortCreatedAt, schemas.SortUpdatedAt)
	}
}

// orDefault returns v when it was supplied, and fallback when it was not.
func orDefault(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

// Get handles GET /api/v1/notes/:id.
func (h *NoteHandler) Get(c *gin.Context) {
	note, _, ok := h.loadOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, toNoteResponse(note))
}

// Update handles PUT /api/v1/notes/:id.
func (h *NoteHandler) Update(c *gin.Context) {
	note, ownerID, ok := h.loadOwned(c)
	if !ok {
		return
	}

	var req schemas.UpdateNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	upd := db.NoteUpdate{Title: req.Title, Body: req.Body, IsPublic: req.IsPublic}
	if req.Tags != nil {
		normalised := utils.NormaliseTags(*req.Tags)
		upd.Tags = &normalised
	}

	// An update naming no fields at all is a client mistake worth reporting,
	// not a silent no-op that returns the unchanged note as though it worked.
	if upd.IsEmpty() {
		respondError(c, http.StatusBadRequest, schemas.CodeInvalidInput,
			"Request must change at least one field", nil)
		return
	}

	updated, err := h.notes.Update(c.Request.Context(), note.ID.Hex(), ownerID, upd)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// The note was deleted between the ownership check and the write.
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Note not found", nil)
			return
		}
		respondInternal(c)
		return
	}

	c.JSON(http.StatusOK, toNoteResponse(updated))
}

// Delete handles DELETE /api/v1/notes/:id.
func (h *NoteHandler) Delete(c *gin.Context) {
	note, ownerID, ok := h.loadOwned(c)
	if !ok {
		return
	}

	if err := h.notes.SoftDelete(c.Request.Context(), note.ID.Hex(), ownerID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Note not found", nil)
			return
		}
		respondInternal(c)
		return
	}

	c.Status(http.StatusNoContent)
}

// callerID returns the authenticated caller's ObjectID, writing the response
// itself and returning false when that is not possible.
func (h *NoteHandler) callerID(c *gin.Context) (bson.ObjectID, bool) {
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

// loadOwned fetches the note named in the path and confirms the caller owns it.
//
// This is the single access-control decision for notes, and every one of the
// four endpoints goes through it, so the rule cannot drift between them.
//
// It answers 404 for a note that does not exist and 403 for one belonging to
// somebody else, which is what api_spec.md's status code summary describes.
// The alternative — 404 in both cases — would hide whether a given ID exists
// at all; that is the stricter choice, but it is not the one the specification
// asks for, and it would leave 403 with no way to occur.
func (h *NoteHandler) loadOwned(c *gin.Context) (*models.Note, bson.ObjectID, bool) {
	ownerID, ok := h.callerID(c)
	if !ok {
		return nil, bson.ObjectID{}, false
	}

	note, err := h.notes.GetByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "Note not found", nil)
			return nil, bson.ObjectID{}, false
		}
		respondInternal(c)
		return nil, bson.ObjectID{}, false
	}

	if note.OwnerID != ownerID {
		respondError(c, http.StatusForbidden, schemas.CodeForbidden,
			"You do not have access to this note", nil)
		return nil, bson.ObjectID{}, false
	}

	return note, ownerID, true
}

// toNoteResponse converts a stored note into its API representation.
func toNoteResponse(n *models.Note) schemas.NoteResponse {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return schemas.NoteResponse{
		ID:        n.ID.Hex(),
		Title:     n.Title,
		Body:      n.Body,
		Tags:      tags,
		IsPublic:  n.IsPublic,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}
