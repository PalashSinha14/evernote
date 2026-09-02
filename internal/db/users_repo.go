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

// UserRepo is the data access layer for the users collection.
type UserRepo struct {
	col *mongo.Collection
}

// NewUserRepo returns a repository over the users collection of db.
func NewUserRepo(db *mongo.Database) *UserRepo {
	return &UserRepo{col: db.Collection(models.UsersCollection)}
}

// ensureUserIndexes creates the unique index on email required by db_schema.md.
//
// Uniqueness is enforced by the database rather than by a read-then-write in
// the signup handler, because two simultaneous signups with the same address
// would both pass such a check and both insert.
func ensureUserIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection(models.UsersCollection).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uniq_email"),
	})
	return err
}

// Create inserts a new user and populates its ID.
//
// A violation of the unique email index comes back as ErrDuplicateEmail, which
// is the only way this repository reports the address as taken.
func (r *UserRepo) Create(ctx context.Context, u *models.User) error {
	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now

	res, err := r.col.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateEmail
		}
		return fmt.Errorf("inserting user: %w", err)
	}

	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		u.ID = id
	}
	return nil
}

// FindByEmail returns the user with the given address, or ErrNotFound.
func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	return r.findOne(ctx, bson.D{{Key: "email", Value: email}})
}

// FindByID returns the user with the given ID, or ErrNotFound. An ID that is
// not valid hex is reported as ErrNotFound rather than as a parse failure:
// from the caller's point of view no such user exists either way.
func (r *UserRepo) FindByID(ctx context.Context, id string) (*models.User, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrNotFound
	}
	return r.findOne(ctx, bson.D{{Key: "_id", Value: oid}})
}

// TouchLastLogin stamps the user's last_login, as db_schema.md specifies.
func (r *UserRepo) TouchLastLogin(ctx context.Context, id bson.ObjectID) error {
	now := time.Now().UTC()
	_, err := r.col.UpdateByID(ctx, id, bson.D{{Key: "$set", Value: bson.D{
		{Key: "last_login", Value: now},
		{Key: "updated_at", Value: now},
	}}})
	if err != nil {
		return fmt.Errorf("updating last_login: %w", err)
	}
	return nil
}

func (r *UserRepo) findOne(ctx context.Context, filter bson.D) (*models.User, error) {
	var u models.User
	if err := r.col.FindOne(ctx, filter).Decode(&u); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("finding user: %w", err)
	}
	return &u, nil
}
