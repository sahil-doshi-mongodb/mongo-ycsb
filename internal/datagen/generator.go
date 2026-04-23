package datagen

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"go.mongodb.org/mongo-driver/bson"
)

// Generator creates documents and manages the key space for benchmark ops.
// It is safe for concurrent use — counters use atomics, and all
// randomness is pushed to per-worker RNG/Faker instances.
type Generator struct {
	cfg          *config.DocumentShapeConfig
	insertedDocs atomic.Int64
	nextKey      atomic.Int64
}

// New creates a Generator. startingCount reflects docs already in the
// collection (e.g. after preload) so reads/updates target valid keys immediately.
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
// Accepts a caller-supplied *rand.Rand to avoid global mutex contention.
// Returns "" when no documents exist yet.
func (g *Generator) RandomExistingKey(rng *rand.Rand) string {
	count := g.insertedDocs.Load()
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("user%d", rng.Int63n(count))
}

// InsertedCount returns the current number of known inserted documents.
func (g *Generator) InsertedCount() int64 {
	return g.insertedDocs.Load()
}

// BuildDocument generates a full document for the given key.
// Accepts caller-supplied rng and faker to avoid shared-mutex contention.
func (g *Generator) BuildDocument(key string, rng *rand.Rand, faker *gofakeit.Faker) bson.M {
	doc := bson.M{"_id": key}

	for i := 0; i < g.cfg.FieldCount; i++ {
		doc[fmt.Sprintf("field%d", i)] = g.generateValue(i, rng, faker)
	}

	if g.cfg.NestedDocs {
		nested := bson.M{}
		for i := 0; i < g.cfg.FieldCount; i++ {
			nested[fmt.Sprintf("nfield%d", i)] = g.generateValue(i, rng, faker)
		}
		doc["nested"] = nested
	}

	if g.cfg.Arrays {
		arr := make([]interface{}, g.cfg.ArraySize)
		for i := range arr {
			arr[i] = g.generateValue(i, rng, faker)
		}
		doc["tags"] = arr
	}

	return doc
}

// BuildUpdateDoc generates a $set targeting one random field.
func (g *Generator) BuildUpdateDoc(rng *rand.Rand, faker *gofakeit.Faker) bson.M {
	idx := rng.Intn(g.cfg.FieldCount)
	return bson.M{
		"$set": bson.M{
			fmt.Sprintf("field%d", idx): g.generateValue(idx, rng, faker),
		},
	}
}

func (g *Generator) generateValue(fieldIndex int, rng *rand.Rand, faker *gofakeit.Faker) interface{} {
	if !g.cfg.UseRealisticData {
		return randomString(g.cfg.FieldSize, rng)
	}
	switch fieldIndex % 6 {
	case 0:
		return faker.Name()
	case 1:
		return faker.Email()
	case 2:
		return faker.City() + ", " + faker.CountryAbr()
	case 3:
		return faker.Date().Format(time.RFC3339)
	case 4:
		return faker.Price(1, 10000)
	default:
		return faker.Sentence(8)
	}
}

func randomString(size int, rng *rand.Rand) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	sb.Grow(size)
	for i := 0; i < size; i++ {
		sb.WriteByte(chars[rng.Intn(len(chars))])
	}
	return sb.String()
}

// NewWorkerRNG creates a seeded, goroutine-local *rand.Rand.
// Each worker gets its own instance to avoid the global rand mutex.
func NewWorkerRNG() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// NewWorkerFaker creates a goroutine-local *gofakeit.Faker.
// Each worker gets its own instance to avoid gofakeit's internal mutex.
func NewWorkerFaker() *gofakeit.Faker {
	return gofakeit.New(time.Now().UnixNano())
}
