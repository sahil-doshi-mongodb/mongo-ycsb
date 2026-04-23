package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yourusername/mongo-ycsb/internal/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

// NewClient creates, verifies, and returns a MongoDB client configured
// for benchmarking (retries disabled, pool pre-sized).
func NewClient(ctx context.Context, cfg *config.ConnectionConfig) (*mongo.Client, error) {
	rp, err := parseReadPreference(cfg.ReadPreference)
	if err != nil {
		return nil, err
	}
	wc, err := parseWriteConcern(cfg.WriteConcern)
	if err != nil {
		return nil, err
	}
	rc := parseReadConcern(cfg.ReadConcern)

	serverSelectionTimeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	// MinPoolSize = threads ensures all connections are established
	// before the benchmark starts, avoiding TLS+auth overhead mid-run.
	// Cap at MaxPoolSize to avoid invalid config.
	minPool := cfg.ConnectionPoolSize
	if minPool > cfg.ConnectionPoolSize {
		minPool = cfg.ConnectionPoolSize
	}

	opts := options.Client().
		ApplyURI(cfg.URI).
		SetReadPreference(rp).
		SetWriteConcern(wc).
		SetReadConcern(rc).
		SetMinPoolSize(minPool).
		SetMaxPoolSize(cfg.ConnectionPoolSize).
		SetServerSelectionTimeout(serverSelectionTimeout).
		// Disable retries for accurate benchmarking — retries silently
		// double latency on transient errors and mask real error rates.
		SetRetryReads(false).
		SetRetryWrites(false)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, rp); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return client, nil
}

// WarmUpPool explicitly opens n connections concurrently so that TLS
// handshake and auth are fully complete before the benchmark clock starts.
func WarmUpPool(ctx context.Context, client *mongo.Client, n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.Ping(ctx, nil)
		}()
	}
	wg.Wait()
}

func parseReadPreference(rp string) (*readpref.ReadPref, error) {
	switch rp {
	case "primary", "":
		return readpref.Primary(), nil
	case "primaryPreferred":
		return readpref.PrimaryPreferred(), nil
	case "secondary":
		return readpref.Secondary(), nil
	case "secondaryPreferred":
		return readpref.SecondaryPreferred(), nil
	case "nearest":
		return readpref.Nearest(), nil
	default:
		return nil, fmt.Errorf("unknown readPreference %q", rp)
	}
}

// parseWriteConcern uses the non-deprecated v1.12+ API.
// The old writeconcern.New() applied unexpected journaling defaults
// in v1.13.x that added significant write latency.
func parseWriteConcern(wc string) (*writeconcern.WriteConcern, error) {
	switch wc {
	case "majority", "":
		return writeconcern.Majority(), nil
	case "w:1", "w1":
		return writeconcern.W1(), nil
	case "w:0", "w0":
		return writeconcern.Unacknowledged(), nil
	default:
		return nil, fmt.Errorf("unknown writeConcern %q", wc)
	}
}

func parseReadConcern(rc string) *readconcern.ReadConcern {
	switch rc {
	case "majority":
		return readconcern.Majority()
	case "linearizable":
		return readconcern.Linearizable()
	case "available":
		return readconcern.Available()
	default:
		return readconcern.Local()
	}
}
