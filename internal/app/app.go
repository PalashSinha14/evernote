// Package app wires the application together: it loads the database, builds
// the repositories and handlers, and registers the routes. It is the single
// place where the layers meet, which keeps that knowledge out of main.
package app

import (
	"context"
	"fmt"

	"github.com/PalashSinha14/evernote/internal/config"
	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/handlers"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/gin-gonic/gin"
)

// App is a fully wired application: a router ready to serve, and the resources
// it owns.
type App struct {
	Router *gin.Engine
	cfg    *config.Config
	mongo  *db.Client
}

// New connects to MongoDB, ensures the indexes exist and builds the router.
//
// Index creation happens at startup rather than in a migration step, because
// it is idempotent in MongoDB and this way a fresh database is correctly shaped
// the first time the service runs against it.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	mongoClient, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return nil, err
	}

	if err := mongoClient.EnsureIndexes(ctx); err != nil {
		_ = mongoClient.Disconnect(context.Background())
		return nil, fmt.Errorf("ensuring indexes: %w", err)
	}

	userRepo := db.NewUserRepo(mongoClient.DB)
	revokedRepo := db.NewRevokedTokenRepo(mongoClient.DB)

	authHandler := handlers.NewAuthHandler(userRepo, revokedRepo, cfg)
	healthHandler := handlers.NewHealthHandler(mongoClient)

	app := &App{cfg: cfg, mongo: mongoClient}
	app.Router = app.buildRouter(authHandler, healthHandler, revokedRepo)
	return app, nil
}

// buildRouter registers every route the service exposes.
func (a *App) buildRouter(
	auth *handlers.AuthHandler,
	health *handlers.HealthHandler,
	revoked *db.RevokedTokenRepo,
) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	// requireAuth is applied per route group rather than globally, because the
	// public share endpoint added in a later phase must stay reachable without
	// a token.
	requireAuth := middleware.RequireAuth(a.cfg.JWTSecret, revoked)

	router.GET("/healthz", health.Healthz)

	v1 := router.Group("/api/v1")
	{
		authGroup := v1.Group("/auth")
		{
			authGroup.POST("/signup", auth.Signup)
			authGroup.POST("/login", auth.Login)
			authGroup.POST("/logout", requireAuth, auth.Logout)
		}

		v1.GET("/me", requireAuth, auth.Me)
	}

	return router
}

// Close releases the application's resources.
func (a *App) Close(ctx context.Context) error {
	return a.mongo.Disconnect(ctx)
}
