package handlers

import (
	"errors"
	"net/http"

	"github.com/PalashSinha14/evernote/internal/config"
	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/PalashSinha14/evernote/internal/models"
	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/PalashSinha14/evernote/internal/utils"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// AuthHandler serves signup, login, logout and the profile endpoint.
type AuthHandler struct {
	users   *db.UserRepo
	revoked *db.RevokedTokenRepo
	cfg     *config.Config
}

// NewAuthHandler wires an AuthHandler to its dependencies.
func NewAuthHandler(users *db.UserRepo, revoked *db.RevokedTokenRepo, cfg *config.Config) *AuthHandler {
	return &AuthHandler{users: users, revoked: revoked, cfg: cfg}
}

// Signup handles POST /api/v1/auth/signup.
func (h *AuthHandler) Signup(c *gin.Context) {
	var req schemas.SignupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	hash, err := utils.HashPassword(req.Password, h.cfg.BcryptCost)
	if err != nil {
		respondInternal(c)
		return
	}

	user := &models.User{
		Name:         req.Name,
		Email:        utils.NormaliseEmail(req.Email),
		PasswordHash: hash,
	}

	// Uniqueness is left to the index on users.email rather than checked here
	// first, because a check-then-insert would let two simultaneous signups
	// for the same address both pass the check.
	if err := h.users.Create(c.Request.Context(), user); err != nil {
		if errors.Is(err, db.ErrDuplicateEmail) {
			respondError(c, http.StatusBadRequest, schemas.CodeInvalidInput,
				"Email already registered", nil)
			return
		}
		respondInternal(c)
		return
	}

	c.JSON(http.StatusCreated, schemas.SignupResponse{
		ID:        user.ID.Hex(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
	})
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req schemas.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondBindError(c, err)
		return
	}

	user, err := h.users.FindByEmail(c.Request.Context(), utils.NormaliseEmail(req.Email))
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		respondInternal(c)
		return
	}

	// An unknown address and a wrong password produce the same 401 with the
	// same message, so that this endpoint cannot be used to discover which
	// email addresses have accounts.
	if user == nil || !utils.CheckPassword(user.PasswordHash, req.Password) {
		respondError(c, http.StatusUnauthorized, schemas.CodeUnauthorized,
			"Invalid email or password", nil)
		return
	}

	token, expiresIn, err := utils.MintToken(h.cfg.JWTSecret, user.ID.Hex(), h.cfg.JWTExpiry)
	if err != nil {
		respondInternal(c)
		return
	}

	// A failure to stamp last_login must not fail the login itself: the caller
	// has proved who they are, and the timestamp is bookkeeping.
	_ = h.users.TouchLastLogin(c.Request.Context(), user.ID)

	c.JSON(http.StatusOK, schemas.LoginResponse{Token: token, ExpiresIn: expiresIn})
}

// Logout handles POST /api/v1/auth/logout.
//
// The token cannot be withdrawn from the client, so it is recorded as revoked
// and the auth middleware refuses it from here on.
func (h *AuthHandler) Logout(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		respondInternal(c)
		return
	}
	jti, expiresAt, ok := middleware.TokenIdentity(c)
	if !ok {
		respondInternal(c)
		return
	}

	oid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		respondInternal(c)
		return
	}

	if err := h.revoked.Revoke(c.Request.Context(), jti, oid, expiresAt); err != nil {
		respondInternal(c)
		return
	}

	c.Status(http.StatusNoContent)
}

// Me handles GET /api/v1/me.
func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.UserID(c)
	if !ok {
		respondInternal(c)
		return
	}

	user, err := h.users.FindByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// The token is valid but its subject is gone — the account was
			// deleted after the token was issued.
			respondError(c, http.StatusNotFound, schemas.CodeNotFound, "User not found", nil)
			return
		}
		respondInternal(c)
		return
	}

	c.JSON(http.StatusOK, schemas.MeResponse{
		ID:        user.ID.Hex(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
		LastLogin: user.LastLogin,
	})
}
