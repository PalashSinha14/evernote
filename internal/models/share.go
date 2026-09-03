package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// SharesCollection is the name of the collection these documents live in.
const SharesCollection = "shares"

// Share is a document in the shares collection: a public, read-only door onto
// one note.
//
// It is its own collection rather than an array embedded in the note, because
// the public lookup runs in the opposite direction from every other query in
// the system. A visitor arrives holding only a token, and the unique index on
// Token turns that into a single indexed hit. Embedded in the note, the same
// lookup would be a scan across every note in the database.
type Share struct {
	ID           bson.ObjectID `bson:"_id,omitempty"           json:"id"`
	NoteID       bson.ObjectID `bson:"note_id"                 json:"-"`
	OwnerID      bson.ObjectID `bson:"owner_id"                json:"-"`
	Token        string        `bson:"token"                   json:"-"`
	PasswordHash string        `bson:"password_hash,omitempty" json:"-"`
	ExpiresAt    *time.Time    `bson:"expires_at,omitempty"    json:"-"`
	CreatedAt    time.Time     `bson:"created_at"              json:"-"`
	Clicks       int64         `bson:"clicks"                  json:"-"`
}

// HasExpired reports whether the share's optional expiry has passed.
func (s *Share) HasExpired(now time.Time) bool {
	return s.ExpiresAt != nil && now.After(*s.ExpiresAt)
}

// RequiresPassword reports whether a caller must present a password to read
// through this share.
func (s *Share) RequiresPassword() bool {
	return s.PasswordHash != ""
}
