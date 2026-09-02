package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PalashSinha14/evernote/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// NoteRepo is the data access layer for the notes collection.
type NoteRepo struct {
	col *mongo.Collection
}

// NewNoteRepo returns a repository over the notes collection of db.
func NewNoteRepo(db *mongo.Database) *NoteRepo {
	return &NoteRepo{col: db.Collection(models.NotesCollection)}
}

// NoteUpdate carries the fields of a partial update.
//
// Every field is a pointer so that "not supplied" is distinguishable from
// "supplied as the zero value". Without this, an update that omitted is_public
// would be indistinguishable from one setting it to false, and PUT with only a
// title would silently make a public note private.
type NoteUpdate struct {
	Title    *string
	Body     *string
	Tags     *[]string
	IsPublic *bool
}

// IsEmpty reports whether the update would change nothing.
func (u NoteUpdate) IsEmpty() bool {
	return u.Title == nil && u.Body == nil && u.Tags == nil && u.IsPublic == nil
}

// ensureNoteIndexes creates the three indexes db_schema.md requires.
func ensureNoteIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection(models.NotesCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Serves the Phase 3 listing query: every note for one owner, most
			// recently updated first. Ordering the keys owner_id then
			// updated_at lets one index both filter and sort.
			Keys:    bson.D{{Key: "owner_id", Value: 1}, {Key: "updated_at", Value: -1}},
			Options: options.Index().SetName("owner_updated"),
		},
		{
			// Backs the ?q= search. MongoDB permits only one text index per
			// collection, so title and body share this one.
			Keys:    bson.D{{Key: "title", Value: "text"}, {Key: "body", Value: "text"}},
			Options: options.Index().SetName("text_title_body"),
		},
		{
			// Multikey index: MongoDB indexes each element of the array, so a
			// filter on one tag is an indexed lookup rather than a scan.
			Keys:    bson.D{{Key: "tags", Value: 1}},
			Options: options.Index().SetName("tags_multikey"),
		},
	})
	return err
}

// Create inserts a new note and populates its ID.
func (r *NoteRepo) Create(ctx context.Context, n *models.Note) error {
	now := time.Now().UTC()
	n.CreatedAt, n.UpdatedAt = now, now
	n.IsDeleted = false
	if n.Tags == nil {
		// Store an empty array rather than null, so that clients always get a
		// list back and the multikey index has something consistent to hold.
		n.Tags = []string{}
	}

	res, err := r.col.InsertOne(ctx, n)
	if err != nil {
		return fmt.Errorf("inserting note: %w", err)
	}
	if id, ok := res.InsertedID.(bson.ObjectID); ok {
		n.ID = id
	}
	return nil
}

// GetByID returns a note that has not been soft deleted, or ErrNotFound.
//
// Ownership is deliberately NOT checked here. The handler needs to tell "no
// such note" apart from "somebody else's note" in order to answer 404 and 403
// correctly, and it cannot do that if the query has already hidden the second
// case.
func (r *NoteRepo) GetByID(ctx context.Context, id string) (*models.Note, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrNotFound
	}

	var n models.Note
	err = r.col.FindOne(ctx, bson.D{
		{Key: "_id", Value: oid},
		{Key: "is_deleted", Value: false},
	}).Decode(&n)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("finding note: %w", err)
	}
	return &n, nil
}

// Update applies a partial update and returns the note as it now stands.
//
// The filter carries owner_id as well as _id even though the handler has
// already checked ownership. That check and this write are two separate round
// trips, and scoping the write to the owner closes the window between them.
func (r *NoteRepo) Update(ctx context.Context, id string, ownerID bson.ObjectID, upd NoteUpdate) (*models.Note, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrNotFound
	}

	set := bson.D{{Key: "updated_at", Value: time.Now().UTC()}}
	if upd.Title != nil {
		set = append(set, bson.E{Key: "title", Value: *upd.Title})
	}
	if upd.Body != nil {
		set = append(set, bson.E{Key: "body", Value: *upd.Body})
	}
	if upd.Tags != nil {
		set = append(set, bson.E{Key: "tags", Value: *upd.Tags})
	}
	if upd.IsPublic != nil {
		set = append(set, bson.E{Key: "is_public", Value: *upd.IsPublic})
	}

	var n models.Note
	err = r.col.FindOneAndUpdate(ctx,
		bson.D{
			{Key: "_id", Value: oid},
			{Key: "owner_id", Value: ownerID},
			{Key: "is_deleted", Value: false},
		},
		bson.D{{Key: "$set", Value: set}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&n)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("updating note: %w", err)
	}
	return &n, nil
}

// SoftDelete marks a note deleted without removing the document.
//
// db_schema.md recommends soft deletion so a note can be recovered within a
// grace period. It also means a share link pointing at a deleted note still
// resolves to a document, so Phase 4 can answer cleanly instead of failing on
// a missing reference.
func (r *NoteRepo) SoftDelete(ctx context.Context, id string, ownerID bson.ObjectID) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrNotFound
	}

	res, err := r.col.UpdateOne(ctx,
		bson.D{
			{Key: "_id", Value: oid},
			{Key: "owner_id", Value: ownerID},
			{Key: "is_deleted", Value: false},
		},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "is_deleted", Value: true},
			{Key: "updated_at", Value: time.Now().UTC()},
		}}},
	)
	if err != nil {
		return fmt.Errorf("deleting note: %w", err)
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// NoteFilter describes a listing query. The handler validates and defaults the
// raw query string into this before the repository sees it, so the repository
// can trust every field — in particular SortField, which is interpolated into
// a sort document and must never be arbitrary client input.
type NoteFilter struct {
	OwnerID   bson.ObjectID
	Query     string
	Tag       string
	Page      int
	Limit     int
	SortField string
	SortDesc  bool
}

// skip returns the number of documents to step over for this page.
func (f NoteFilter) skip() int64 {
	return int64((f.Page - 1) * f.Limit)
}

// criteria builds the MongoDB filter document shared by the listing query and
// its count.
//
// owner_id is always present, so a listing can only ever see the caller's own
// notes — the scoping is in the query itself, not a check applied to the
// results afterwards.
func (f NoteFilter) criteria() bson.D {
	c := bson.D{
		{Key: "owner_id", Value: f.OwnerID},
		{Key: "is_deleted", Value: false},
	}
	if f.Query != "" {
		c = append(c, bson.E{Key: "$text", Value: bson.D{{Key: "$search", Value: f.Query}}})
	}
	if f.Tag != "" {
		// An equality match against an array field matches documents where any
		// element equals the value, which is exactly the tag filter, and it
		// uses the multikey index.
		c = append(c, bson.E{Key: "tags", Value: f.Tag})
	}
	return c
}

// List returns one page of the caller's notes together with the total number
// that match, which the meta block reports.
//
// The count runs against the same criteria but ignores skip and limit, so
// meta.total is the size of the whole result set rather than of the page. A
// client needs that to know how many pages there are.
func (r *NoteRepo) List(ctx context.Context, f NoteFilter) ([]models.Note, int64, error) {
	criteria := f.criteria()

	order := 1
	if f.SortDesc {
		order = -1
	}

	cursor, err := r.col.Find(ctx, criteria, options.Find().
		SetSort(bson.D{{Key: f.SortField, Value: order}}).
		SetSkip(f.skip()).
		SetLimit(int64(f.Limit)))
	if err != nil {
		return nil, 0, fmt.Errorf("listing notes: %w", err)
	}
	defer cursor.Close(ctx)

	notes := []models.Note{}
	if err := cursor.All(ctx, &notes); err != nil {
		return nil, 0, fmt.Errorf("decoding notes: %w", err)
	}

	total, err := r.col.CountDocuments(ctx, criteria)
	if err != nil {
		return nil, 0, fmt.Errorf("counting notes: %w", err)
	}

	return notes, total, nil
}

// DistinctTags returns every tag used across the caller's live notes, sorted.
//
// api_spec.md calls this a simple aggregation over notes, and Distinct is the
// smallest thing that does it: MongoDB flattens the tags arrays and returns
// each value once. Because tags were normalised on the way in, the result
// needs no further cleaning. Sorting is done here rather than in the database
// so the endpoint returns a stable order.
func (r *NoteRepo) DistinctTags(ctx context.Context, ownerID bson.ObjectID) ([]string, error) {
	res := r.col.Distinct(ctx, "tags", bson.D{
		{Key: "owner_id", Value: ownerID},
		{Key: "is_deleted", Value: false},
	})

	tags := []string{}
	if err := res.Decode(&tags); err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	sort.Strings(tags)
	return tags, nil
}
