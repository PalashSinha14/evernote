package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PalashSinha14/evernote/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ShareRepo is the data access layer for the shares collection.
type ShareRepo struct {
	col *mongo.Collection
}

// NewShareRepo returns a repository over the shares collection of db.
func NewShareRepo(db *mongo.Database) *ShareRepo {
	return &ShareRepo{col: db.Collection(models.SharesCollection)}
}

// ensureShareIndexes creates the two indexes db_schema.md requires: a unique
// index on token, which is what makes the public lookup a single indexed hit
// rather than a scan, and an index on note_id.
func ensureShareIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection(models.SharesCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "token", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_token"),
		},
		{
			Keys:    bson.D{{Key: "note_id", Value: 1}},
			Options: options.Index().SetName("note_id_idx"),
		},
	})
	return err
}

// Create inserts a new share and populates its ID.
//
// A collision on the unique token index comes back as ErrDuplicateToken. With
// a 16-byte random token the odds of that are negligible, but the caller — not
// this repository — decides how to respond to it, typically by minting a fresh
// token and retrying once.
func (r *ShareRepo) Create(ctx context.Context, s *models.Share) error {
	s.CreatedAt = time.Now().UTC()
	s.Clicks = 0

	res, err := r.col.InsertOne(ctx, s)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateToken
		}
		return fmt.Errorf("inserting share: %w", err)
	}
	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		s.ID = id
	}
	return nil
}

// FindByToken returns the share with the given token, or ErrNotFound.
//
// Expiry is deliberately not part of this query. An expired share is a real
// document with something specific to say to the visitor — "this link has
// expired" — which is different from no document existing at all, and the
// handler needs to tell the two apart to answer with the right message.
func (r *ShareRepo) FindByToken(ctx context.Context, token string) (*models.Share, error) {
	var s models.Share
	err := r.col.FindOne(ctx, bson.D{{Key: "token", Value: token}}).Decode(&s)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("finding share: %w", err)
	}
	return &s, nil
}

// IncrementClicks records one more read through this share.
//
// This runs after the note has already been served, so a failure here must
// never be allowed to fail the read itself — a visitor's access to a note
// cannot depend on whether a counter update succeeded. The caller logs the
// error and moves on rather than treating it as request-fatal.
func (r *ShareRepo) IncrementClicks(ctx context.Context, id bson.ObjectID) error {
	_, err := r.col.UpdateByID(ctx, id, bson.D{{Key: "$inc", Value: bson.D{{Key: "clicks", Value: 1}}}})
	if err != nil {
		return fmt.Errorf("incrementing clicks: %w", err)
	}
	return nil
}
