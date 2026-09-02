// Package db is the data access layer. Every MongoDB query in this service
// lives here and nowhere else, so the handlers stay free of driver types and
// can be tested without a database running.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Domain errors. Repositories translate driver errors into these so that the
// handler layer never has to import the Mongo driver to interpret a failure.
var (
	// ErrNotFound means the requested document does not exist.
	ErrNotFound = errors.New("resource not found")
	// ErrDuplicateEmail means a unique index on users.email rejected a write.
	ErrDuplicateEmail = errors.New("email already registered")
)

// Client wraps the Mongo connection and the handful of things the application
// needs from it.
type Client struct {
	mongo *mongo.Client
	DB    *mongo.Database
}

// Connect dials MongoDB and verifies the connection with a ping. Returning
// only after a successful ping means a misconfigured MONGO_URI fails at
// startup rather than on the first request.
func Connect(ctx context.Context, uri, dbName string) (*Client, error) {
	client, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("connecting to mongo: %w", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("pinging mongo: %w", err)
	}

	return &Client{mongo: client, DB: client.Database(dbName)}, nil
}

// Ping checks that the database is still reachable. It backs the health check.
func (c *Client) Ping(ctx context.Context) error {
	return c.mongo.Ping(ctx, readpref.Primary())
}

// Disconnect closes the connection pool.
func (c *Client) Disconnect(ctx context.Context) error {
	return c.mongo.Disconnect(ctx)
}

// EnsureIndexes creates every index the current feature set requires.
//
// It runs on every boot. Index creation in MongoDB is idempotent: an index
// that already exists with the same specification is left alone, so this is
// safe to repeat and means a fresh database is correctly shaped without a
// separate migration step.
//
// Later phases add the notes and shares indexes here.
func (c *Client) EnsureIndexes(ctx context.Context) error {
	if err := ensureUserIndexes(ctx, c.DB); err != nil {
		return fmt.Errorf("users indexes: %w", err)
	}
	if err := ensureRevokedTokenIndexes(ctx, c.DB); err != nil {
		return fmt.Errorf("revoked_tokens indexes: %w", err)
	}
	return nil
}
