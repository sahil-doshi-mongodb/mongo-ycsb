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

// NewBenchmarkClient creates a client optimised for accurate benchmarking.
// Retries are disabled so every error is real and every latency measurement
// reflects exactly one attempt.
func NewBenchmarkClient(ctx context.Context, cfg *config.ConnectionConfig) (*mongo.Client, error) {
	opts, err := baseOptions(cfg)
	if err != nil {
		return nil, err
	}

	opts.
		SetMaxPoolSize(cfg.ConnectionPoolSize).
		SetRetryReads(false).
		SetRetryWrites(false)

	return connect(ctx, opts, cfg)
}

// NewPreloadClient creates a client optimised for bulk data loading.
// Retries are enabled so transient connection hiccups during long
// bulk insert operations don't abort the entire preload.
// Pool is kept small — preload uses its own goroutines, not benchmark threads.
func NewPreloadClient(ctx context.Context, cfg *config.ConnectionConfig) (*mongo.Client, error) {
	opts, err := baseOptions(cfg)
	if err != nil {
		return nil, err
	}

	opts.
		SetMaxPoolSize(50). // preload never needs more than its thread count
		SetRetryReads(true).
		SetRetryWrites(true)

	return connect(ctx, opts, cfg)
}

// baseOptions builds the common options shared by both client types.
func baseOptions(cfg *config.ConnectionConfig) (*options.ClientOptions, error) {
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

	return options.Client().
		ApplyURI(cfg.URI).
		SetReadPreference(rp).
		SetWriteConcern(wc).
		SetReadConcern(rc).
		SetServerSelectionTimeout(serverSelectionTimeout), nil
}

// connect dials MongoDB, pings it, and returns the client.
func connect(ctx context.Context, opts *options.ClientOptions, cfg *config.ConnectionConfig) (*mongo.Client, error) {
	rp, err := parseReadPreference(cfg.ReadPreference)
	if err != nil {
		return nil, err
	}

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

// WarmUpPool opens n connections in small batches to avoid overwhelming
// the server with simultaneous TLS handshakes.
func WarmUpPool(ctx context.Context, client *mongo.Client, n int) {
	const batchSize = 10
	var wg sync.WaitGroup

	for i := 0; i < n; i += batchSize {
		end := i + batchSize
		if end > n {
			end = n
		}
		for j := i; j < end; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = client.Ping(ctx, nil)
			}()
		}
		wg.Wait()
	}
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
