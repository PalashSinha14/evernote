package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// RevokedTokensCollection is the name of the collection these documents live in.
const RevokedTokensCollection = "revoked_tokens"

// RevokedToken records a single JWT that has been logged out.
//
// JWTs are stateless: once signed, a token stays valid until it expires, and
// nothing in the token itself can be changed afterwards. Logout therefore has
// to be recorded on the server side, and the auth middleware consults this
// collection on every request.
//
// Only the token's jti (its unique identifier) is stored, never the token
// itself, so this collection is useless to anybody who reads it. ExpiresAt
// carries a TTL index, so each row deletes itself once the token it revokes
// would have expired anyway — the collection stays bounded on its own.
type RevokedToken struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	JTI       string        `bson:"jti"           json:"jti"`
	UserID    bson.ObjectID `bson:"user_id"       json:"user_id"`
	ExpiresAt time.Time     `bson:"expires_at"    json:"expires_at"`
	RevokedAt time.Time     `bson:"revoked_at"    json:"revoked_at"`
}
