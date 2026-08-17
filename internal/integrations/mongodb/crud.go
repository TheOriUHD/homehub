package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CollIndex is one index on a collection.
type CollIndex struct {
	Name   string   `json:"name"`
	Keys   []string `json:"keys"`
	Unique bool     `json:"unique"`
}

// CollSchema is what structure MongoDB exposes for a collection: its indexes
// and a sampled key set (documents are schemaless).
type CollSchema struct {
	Indexes    []CollIndex `json:"indexes"`
	SampleKeys []string    `json:"sampleKeys"`
	Documents  int64       `json:"documents"`
}

// DocPage is a page of documents.
type DocPage struct {
	Documents  []string `json:"documents"` // canonical extended JSON
	Total      int64    `json:"total"`
	Limit      int      `json:"limit"`
	Skip       int      `json:"skip"`
	DurationMS float64  `json:"durationMs"`
}

// CollectionSchema returns indexes plus keys sampled from recent documents.
func (c *Conn) CollectionSchema(ctx context.Context, database, collection string) (*CollSchema, error) {
	coll := c.client.Database(database).Collection(collection)
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	schema := &CollSchema{Indexes: []CollIndex{}, SampleKeys: []string{}}
	for cursor.Next(ctx) {
		var raw struct {
			Name   string `bson:"name"`
			Key    bson.D `bson:"key"`
			Unique bool   `bson:"unique"`
		}
		if err := cursor.Decode(&raw); err != nil {
			continue
		}
		idx := CollIndex{Name: raw.Name, Unique: raw.Unique}
		for _, k := range raw.Key {
			idx.Keys = append(idx.Keys, k.Key)
		}
		schema.Indexes = append(schema.Indexes, idx)
	}

	// Sample keys from the newest few documents so the UI can seed an
	// insert/edit template without imposing a schema.
	sampleCursor, err := coll.Find(ctx, bson.D{}, options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(20))
	if err == nil {
		defer sampleCursor.Close(ctx)
		seen := map[string]bool{}
		for sampleCursor.Next(ctx) {
			var doc bson.D
			if err := sampleCursor.Decode(&doc); err != nil {
				continue
			}
			for _, e := range doc {
				if !seen[e.Key] {
					seen[e.Key] = true
					schema.SampleKeys = append(schema.SampleKeys, e.Key)
				}
			}
		}
	}
	schema.Documents, _ = coll.EstimatedDocumentCount(ctx)
	return schema, nil
}

// parseFilter turns an optional extended-JSON filter string into a document.
func parseFilter(filter string) (bson.D, error) {
	if filter == "" {
		return bson.D{}, nil
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(filter), false, &doc); err != nil {
		return nil, fmt.Errorf("filter is not valid JSON: %w", err)
	}
	return doc, nil
}

// Documents reads a page with an optional filter and sort field.
func (c *Conn) Documents(ctx context.Context, database, collection string,
	limit, skip int, filter, sortField, sortDir string) (*DocPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if skip < 0 {
		skip = 0
	}
	query, err := parseFilter(filter)
	if err != nil {
		return nil, err
	}
	coll := c.client.Database(database).Collection(collection)

	total, err := coll.CountDocuments(ctx, query)
	if err != nil {
		return nil, err
	}
	findOpts := options.Find().SetLimit(int64(limit)).SetSkip(int64(skip))
	if sortField != "" {
		dir := 1
		if sortDir == "desc" {
			dir = -1
		}
		findOpts.SetSort(bson.D{{Key: sortField, Value: dir}})
	} else {
		findOpts.SetSort(bson.D{{Key: "_id", Value: -1}})
	}

	start := time.Now()
	cursor, err := coll.Find(ctx, query, findOpts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	page := &DocPage{Documents: []string{}, Total: total, Limit: limit, Skip: skip}
	for cursor.Next(ctx) {
		raw, err := bson.MarshalExtJSON(cursor.Current, false, false)
		if err != nil {
			continue
		}
		page.Documents = append(page.Documents, string(raw))
	}
	page.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	return page, cursor.Err()
}

// InsertDocument inserts one document from an extended-JSON string.
func (c *Conn) InsertDocument(ctx context.Context, database, collection, docJSON string) error {
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), false, &doc); err != nil {
		return fmt.Errorf("document is not valid JSON: %w", err)
	}
	_, err := c.client.Database(database).Collection(collection).InsertOne(ctx, doc)
	return err
}

// idFilter builds the {_id: …} filter from an extended-JSON _id value.
func idFilter(idJSON string) (bson.D, error) {
	if idJSON == "" {
		return nil, errors.New("_id is required to identify the document")
	}
	var wrapper struct {
		ID bson.RawValue `bson:"_id"`
	}
	if err := bson.UnmarshalExtJSON([]byte(`{"_id":`+idJSON+`}`), false, &wrapper); err != nil {
		return nil, fmt.Errorf("_id is not valid JSON: %w", err)
	}
	return bson.D{{Key: "_id", Value: wrapper.ID}}, nil
}

// ReplaceDocument replaces the document with the given _id. The replacement
// JSON must itself contain the same _id (Mongo forbids changing it).
func (c *Conn) ReplaceDocument(ctx context.Context, database, collection, idJSON, docJSON string) (int64, error) {
	filter, err := idFilter(idJSON)
	if err != nil {
		return 0, err
	}
	var doc bson.D
	if err := bson.UnmarshalExtJSON([]byte(docJSON), false, &doc); err != nil {
		return 0, fmt.Errorf("document is not valid JSON: %w", err)
	}
	res, err := c.client.Database(database).Collection(collection).ReplaceOne(ctx, filter, doc)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

// DeleteDocument deletes the document with the given _id.
func (c *Conn) DeleteDocument(ctx context.Context, database, collection, idJSON string) (int64, error) {
	filter, err := idFilter(idJSON)
	if err != nil {
		return 0, err
	}
	res, err := c.client.Database(database).Collection(collection).DeleteOne(ctx, filter)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// CreateCollection creates an empty collection (which also materializes the
// database when it is new).
func (c *Conn) CreateCollection(ctx context.Context, database, collection string) error {
	if collection == "" || len(collection) > 120 {
		return errors.New("collection name must be 1–120 characters")
	}
	return c.client.Database(database).CreateCollection(ctx, collection)
}

// DropCollection removes a collection.
func (c *Conn) DropCollection(ctx context.Context, database, collection string) error {
	return c.client.Database(database).Collection(collection).Drop(ctx)
}
