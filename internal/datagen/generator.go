package datagen

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"go.mongodb.org/mongo-driver/bson"
)

// Generator creates documents and manages the key space for benchmark ops.
// Keys follow the pattern "user0", "user1", ..., "userN" — matching
// standard YCSB behaviour and making range scans predictable.
type Generator struct {
	cfg          *config.DocumentShapeConfig
	insertedDocs atomic.Int64 // docs known to exist (preload + inserts)
	nextKey      atomic.Int64 // monotonically increasing insert key
}

// New creates a Generator. startingCount is the number of docs already
// in the collection (e.g. inserted during preload) so reads/updates
// immediately target valid keys.
func New(cfg *config.DocumentShapeConfig, startingCount int64) *Generator {
	g := &Generator{cfg: cfg}
	g.insertedDocs.Store(startingCount)
	g.nextKey.Store(startingCount)
	return g
}

// NextInsertKey returns the next sequential key and increments counters.
func (g *Generator) NextInsertKey() string {
	k := g.nextKey.Add(1) - 1
	g.insertedDocs.Add(1)
	return fmt.Sprintf("user%d", k)
}

// RandomExistingKey picks a uniform-random key from the known key space.
// Returns "" when no documents exist yet (e.g. before preload).
func (g *Generator) RandomExistingKey() string {
	count := g.insertedDocs.Load()
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("user%d", rand.Int63n(count))
}

// InsertedCount returns the current number of known inserted documents.
func (g *Generator) InsertedCount() int64 {
	return g.insertedDocs.Load()
}

// BuildDocument generates a full document for the given key.
func (g *Generator) BuildDocument(key string) bson.M {
	doc := bson.M{"_id": key}

	for i := 0; i < g.cfg.FieldCount; i++ {
		doc[fmt.Sprintf("field%d", i)] = g.generateValue(i)
	}

	if g.cfg.NestedDocs {
		nested := bson.M{}
		for i := 0; i < g.cfg.FieldCount; i++ {
			nested[fmt.Sprintf("nfield%d", i)] = g.generateValue(i)
		}
		doc["nested"] = nested
	}

	if g.cfg.Arrays {
		arr := make([]interface{}, g.cfg.ArraySize)
		for i := range arr {
			arr[i] = g.generateValue(i)
		}
		doc["tags"] = arr
	}

	return doc
}

// BuildUpdateDoc generates a $set targeting one random field.
func (g *Generator) BuildUpdateDoc() bson.M {
	idx := rand.Intn(g.cfg.FieldCount)
	return bson.M{
		"$set": bson.M{
			fmt.Sprintf("field%d", idx): g.generateValue(idx),
		},
	}
}

// generateValue rotates through realistic data types based on field index.
func (g *Generator) generateValue(fieldIndex int) interface{} {
	if !g.cfg.UseRealisticData {
		return randomString(g.cfg.FieldSize)
	}
	switch fieldIndex % 6 {
	case 0:
		return gofakeit.Name()
	case 1:
		return gofakeit.Email()
	case 2:
		return gofakeit.City() + ", " + gofakeit.CountryAbr()
	case 3:
		return gofakeit.Date().Format("2006-01-02T15:04:05Z")
	case 4:
		return gofakeit.Price(1, 10000)
	default:
		return gofakeit.Sentence(8)
	}
}

func randomString(size int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	sb.Grow(size)
	for i := 0; i < size; i++ {
		sb.WriteByte(chars[rand.Intn(len(chars))])
	}
	return sb.String()
}
