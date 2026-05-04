package datagen

import (
	"fmt"
	"math/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/yourusername/mongo-ycsb/internal/config"
	"github.com/yourusername/mongo-ycsb/internal/distribution"
	"go.mongodb.org/mongo-driver/bson"
)

// Generator creates documents and manages the key space for benchmark ops.
// All randomness is pushed to per-worker RNG/Faker instances to avoid
// shared-mutex contention.
type Generator struct {
	shapeCfg    *config.DocumentShapeConfig
	execCfg     *config.ExecutionConfig
	workloadCfg *config.WorkloadConfig

	nextKey      atomic.Int64
	insertedDocs atomic.Int64
	seqCounter   atomic.Int64

	zipf        *distribution.ScrambledZipfian // Zipfian distribution
	latestZipf  *distribution.ScrambledZipfian // Latest distribution (Workload D)
	recordCount int64
}

// New creates a Generator.
// startingCount is the number of documents already in the collection
// (e.g. inserted during preload) — both counters start at this value.
func New(
	shapeCfg *config.DocumentShapeConfig,
	execCfg *config.ExecutionConfig,
	workloadCfg *config.WorkloadConfig,
	startingCount int64,
) *Generator {
	g := &Generator{
		shapeCfg:    shapeCfg,
		execCfg:     execCfg,
		workloadCfg: workloadCfg,
		recordCount: startingCount,
	}
	g.insertedDocs.Store(startingCount)
	g.nextKey.Store(startingCount)

	// Override recordCount from config if set
	if execCfg.RecordCount > 0 {
		g.recordCount = execCfg.RecordCount
	}

	// Pre-build Zipfian generator if needed (done once — O(10k) cost)
	if execCfg.KeyDistribution == "zipfian" && g.recordCount > 0 {
		fmt.Printf("📐 Initialising Zipfian distribution (n=%d, θ=%.3f)...\n",
			g.recordCount, execCfg.EffectiveZipfianConstant())
		g.zipf = distribution.NewScrambledZipfian(g.recordCount, execCfg.EffectiveZipfianConstant())
		fmt.Printf("   ✅ Zipfian ready\n\n")
	}

	if execCfg.KeyDistribution == "latest" && g.recordCount > 0 {
		// Matches YCSB's SkewedLatestGenerator — Zipfian offset subtracted
		// from the most recently inserted key. Initialised with recordCount
		// as n; clamped to actual insertedCount at query time.
		fmt.Printf("📐 Initialising Latest distribution (n=%d, θ=%.3f)...\n",
			g.recordCount, execCfg.EffectiveZipfianConstant())
		g.latestZipf = distribution.NewScrambledZipfian(g.recordCount, execCfg.EffectiveZipfianConstant())
		fmt.Printf("   ✅ Latest ready\n\n")
	}

	return g
}

// ── Insert key management ─────────────────────────────────────────────────────

// ReserveInsertKey reserves the next sequential key for an upcoming insert.
// The key is NOT yet available for reads/updates — call AcknowledgeInsert after
// the insert succeeds. This matches YCSB's AcknowledgedCounterGenerator.
func (g *Generator) ReserveInsertKey() string {
	k := g.nextKey.Add(1) - 1
	return g.formatKey(k)
}

// AcknowledgeInsert marks one insert as complete, making the key available
// for subsequent reads and updates.
func (g *Generator) AcknowledgeInsert() {
	g.insertedDocs.Add(1)
}

// InsertedCount returns the count of acknowledged inserts.
func (g *Generator) InsertedCount() int64 {
	return g.insertedDocs.Load()
}

// ── Existing key selection ────────────────────────────────────────────────────

// NextExistingKey returns a key from the existing key space using the
// configured distribution. Returns "" if no documents exist yet.
func (g *Generator) NextExistingKey(rng *rand.Rand) string {
	count := g.insertedDocs.Load()
	if count == 0 {
		return ""
	}

	var idx int64
	switch g.execCfg.KeyDistribution {
	case "zipfian":
		if g.zipf != nil {
			// Zipfian samples from the fixed recordCount key space
			idx = g.zipf.Next(rng)
			// Clamp to actual inserted range in case recordCount > insertedDocs
			if idx >= count {
				idx = count - 1
			}
		} else {
			idx = rng.Int63n(count)
		}
	case "latest":
		// Zipfian offset from most recently inserted key — matches YCSB SkewedLatestGenerator
		if g.latestZipf != nil {
			idx = distribution.NextLatest(rng, count, g.latestZipf)
		} else {
			// Fallback if generator wasn't initialised (e.g. recordCount was 0 at init time)
			idx = rng.Int63n(count)
		}
	case "sequential":
		// Rotate sequentially through all existing keys
		n := g.seqCounter.Add(1) - 1
		idx = n % count
	default: // uniform
		idx = rng.Int63n(count)
	}

	return g.formatKey(idx)
}

// ── Document construction ─────────────────────────────────────────────────────

// BuildDocument generates a full document for the given key.
func (g *Generator) BuildDocument(key string, rng *rand.Rand, faker *gofakeit.Faker) bson.M {
	doc := bson.M{"_id": key}

	for i := 0; i < g.shapeCfg.FieldCount; i++ {
		doc[fmt.Sprintf("field%d", i)] = g.generateValue(i, rng, faker)
	}

	if g.shapeCfg.NestedDocs {
		nested := bson.M{}
		for i := 0; i < g.shapeCfg.FieldCount; i++ {
			nested[fmt.Sprintf("nfield%d", i)] = g.generateValue(i, rng, faker)
		}
		doc["nested"] = nested
	}

	if g.shapeCfg.Arrays {
		arr := make([]interface{}, g.shapeCfg.ArraySize)
		for i := range arr {
			arr[i] = g.generateValue(i, rng, faker)
		}
		doc["tags"] = arr
	}

	return doc
}

// BuildUpdateDoc generates a $set update.
// If writeAllFields (YCSB writeallfields=true): updates every field.
// Otherwise (YCSB default): updates one random field.
func (g *Generator) BuildUpdateDoc(rng *rand.Rand, faker *gofakeit.Faker) bson.M {
	setDoc := bson.M{}

	if g.workloadCfg != nil && g.workloadCfg.WriteAllFields {
		for i := 0; i < g.shapeCfg.FieldCount; i++ {
			setDoc[fmt.Sprintf("field%d", i)] = g.generateValue(i, rng, faker)
		}
	} else {
		idx := rng.Intn(g.shapeCfg.FieldCount)
		setDoc[fmt.Sprintf("field%d", idx)] = g.generateValue(idx, rng, faker)
	}

	return bson.M{"$set": setDoc}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// formatKey builds a key string with optional zero-padding, matching YCSB format.
// YCSB default: "user<N>" with no padding.
// With zeroPadding=12: "user000000000042"
func (g *Generator) formatKey(n int64) string {
	prefix := g.execCfg.EffectiveKeyPrefix()
	pad := g.execCfg.KeyZeroPadding
	if pad > 0 {
		return fmt.Sprintf("%s%0*d", prefix, pad, n)
	}
	return fmt.Sprintf("%s%d", prefix, n)
}

// generateValue produces a field value of exactly shapeCfg.FieldSize bytes.
// If UseRealisticData=false: random ASCII bytes (matches YCSB default behaviour).
// If UseRealisticData=true: gofakeit value padded/truncated to fieldSize.
func (g *Generator) generateValue(fieldIndex int, rng *rand.Rand, faker *gofakeit.Faker) interface{} {
	size := g.shapeCfg.FieldSize
	if size <= 0 {
		size = 100
	}

	if !g.shapeCfg.UseRealisticData {
		// YCSB default: random bytes of exactly fieldSize length
		return randomString(size, rng)
	}

	// Realistic data — padded/truncated to exactly fieldSize bytes
	var raw string
	switch fieldIndex % 6 {
	case 0:
		raw = faker.Name()
	case 1:
		raw = faker.Email()
	case 2:
		raw = faker.City() + ", " + faker.CountryAbr()
	case 3:
		raw = faker.Date().Format(time.RFC3339)
	case 4:
		raw = fmt.Sprintf("%.2f", faker.Price(1, 10000))
	default:
		raw = faker.Sentence(8)
	}

	return padOrTruncate(raw, size)
}

// padOrTruncate ensures the string is exactly size bytes.
func padOrTruncate(s string, size int) string {
	if len(s) >= size {
		return s[:size]
	}
	return s + strings.Repeat(" ", size-len(s))
}

// randomString generates a random ASCII string of exactly size bytes.
func randomString(size int, rng *rand.Rand) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	sb.Grow(size)
	for i := 0; i < size; i++ {
		sb.WriteByte(chars[rng.Intn(len(chars))])
	}
	return sb.String()
}

// ── Per-worker RNG/Faker factories ────────────────────────────────────────────

// NewWorkerRNG creates a seeded, goroutine-local *rand.Rand.
func NewWorkerRNG() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

// NewWorkerFaker creates a goroutine-local *gofakeit.Faker.
func NewWorkerFaker() *gofakeit.Faker {
	return gofakeit.New(time.Now().UnixNano())
}

// ── Hashed insert ordering ────────────────────────────────────────────────────

// ShuffledKeyIndices returns indices [0, n) in a pseudo-random order suitable
// for hashed (non-sequential) preload insertion. Avoids hotspots on sharded
// clusters. Uses Fisher-Yates shuffle seeded from the provided rng.
func ShuffledKeyIndices(n int64, rng *rand.Rand) []int64 {
	indices := make([]int64, n)
	for i := int64(0); i < n; i++ {
		indices[i] = i
	}
	rng.Shuffle(int(n), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})
	return indices
}
