package db

import (
	"context"
	"fmt"
	"time"

	"github.com/PalashSinha14/evernote/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// RevokedTokenRepo is the data access layer for logged-out tokens.
type RevokedTokenRepo struct {
	col *mongo.Collection
}

// NewRevokedTokenRepo returns a repository over the revoked_tokens collection.
func NewRevokedTokenRepo(db *mongo.Database) *RevokedTokenRepo {
	return &RevokedTokenRepo{col: db.Collection(models.RevokedTokensCollection)}
}

// ensureRevokedTokenIndexes creates a unique index on jti and a TTL index on
// expires_at.
//
// The TTL index is what keeps this collection from growing without bound.
// SetExpireAfterSeconds(0) tells MongoDB to delete each document as soon as the
// time in expires_at passes — which is exactly the moment the revoked token
// would have expired on its own and stopped being worth remembering.
func ensureRevokedTokenIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection(models.RevokedTokensCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "jti", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_jti"),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("ttl_expires_at"),
		},
	})
	return err
}

// Revoke records a token as logged out.
//
// Revoking a token that is already revoked is not an error. Logging out twice
// with the same token is a harmless thing for a client to do, and the second
// call should still report success.
func (r *RevokedTokenRepo) Revoke(ctx context.Context, jti string, userID bson.ObjectID, expiresAt time.Time) error {
	_, err := r.col.InsertOne(ctx, models.RevokedToken{
		JTI:       jti,
		UserID:    userID,
		ExpiresAt: expiresAt,
		RevokedAt: time.Now().UTC(),
	})
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("revoking token: %w", err)
	}
	return nil
}

// IsRevoked reports whether the token with this jti has been logged out.
//
// This runs on every authenticated request, which is the cost of being able to
// revoke a stateless token at all. The unique index on jti keeps it to a
// single indexed lookup.
func (r *RevokedTokenRepo) IsRevoked(ctx context.Context, jti string) (bool, error) {
	count, err := r.col.CountDocuments(ctx, bson.D{{Key: "jti", Value: jti}}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("checking revoked token: %w", err)
	}
	return count > 0, nil
}
