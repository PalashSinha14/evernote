package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// NotesCollection is the name of the collection these documents live in.
const NotesCollection = "notes"

// Note is a document in the notes collection.
//
// OwnerID is tagged json:"-" because it is never part of a response. A caller
// only ever sees their own notes, so echoing the owner back tells them nothing
// they did not already know, and keeping it out means a user ID cannot leak
// through the public share endpoint added in Phase 4.
//
// IsDeleted implements the soft delete db_schema.md recommends: the document
// stays, and every read filters it out.
type Note struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	OwnerID   bson.ObjectID `bson:"owner_id"      json:"-"`
	Title     string        `bson:"title"         json:"title"`
	Body      string        `bson:"body"          json:"body"`
	Tags      []string      `bson:"tags"          json:"tags"`
	IsPublic  bool          `bson:"is_public"     json:"is_public"`
	IsDeleted bool          `bson:"is_deleted"    json:"-"`
	CreatedAt time.Time     `bson:"created_at"    json:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"    json:"updated_at"`
}
