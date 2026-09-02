// Package models holds the MongoDB document types. The bson tags are the
// contract with the database and match db_schema.md field for field.
package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// UsersCollection is the name of the collection these documents live in.
const UsersCollection = "users"

// User is a document in the users collection.
//
// PasswordHash carries a json:"-" tag so that a User can never be serialised
// into a response by accident. The plaintext password is never stored at all.
type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"   json:"id"`
	Name         string        `bson:"name"            json:"name"`
	Email        string        `bson:"email"           json:"email"`
	PasswordHash string        `bson:"password_hash"   json:"-"`
	CreatedAt    time.Time     `bson:"created_at"      json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at"      json:"updated_at"`
	LastLogin    *time.Time    `bson:"last_login,omitempty" json:"last_login,omitempty"`
}
