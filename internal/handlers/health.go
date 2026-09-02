package handlers

import (
	"net/http"

	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/gin-gonic/gin"
)

// HealthHandler reports whether the service and its database are usable.
type HealthHandler struct {
	client *db.Client
}

// NewHealthHandler wires a HealthHandler to the Mongo client.
func NewHealthHandler(client *db.Client) *HealthHandler {
	return &HealthHandler{client: client}
}

// Healthz handles GET /healthz.
//
// It pings MongoDB rather than only reporting that the process is running,
// because a server that cannot reach its database can serve nothing useful and
// should not be reported as healthy.
func (h *HealthHandler) Healthz(c *gin.Context) {
	if err := h.client.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "degraded", "database": "unreachable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
}
