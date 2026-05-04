package reporter

import (
	"context"
	"fmt"
	"time"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/config"
	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

// MongoReporter stores RunResult documents in a MongoDB collection.
type MongoReporter struct {
	cfg *config.MongoResultsConfig
}

// NewMongoReporter creates a MongoReporter.
func NewMongoReporter(cfg *config.MongoResultsConfig) *MongoReporter {
	return &MongoReporter{cfg: cfg}
}

// Save writes the RunResult to the configured MongoDB results collection.
func (r *MongoReporter) Save(ctx context.Context, result *models.RunResult) error {
	if !r.cfg.Enabled {
		return nil
	}

	client, err := mongo.Connect(ctx, options.Client().
		ApplyURI(r.cfg.URI).
		SetWriteConcern(writeconcern.Majority()).
		SetServerSelectionTimeout(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("results MongoDB connect failed: %w", err)
	}
	defer client.Disconnect(ctx)

	coll := client.Database(r.cfg.Database).Collection(r.cfg.Collection)

	_, err = coll.InsertOne(ctx, result)
	if err != nil {
		return fmt.Errorf("failed to insert run result: %w", err)
	}

	fmt.Printf("💾 Result saved to MongoDB — %s.%s (run_id: %s)\n",
		r.cfg.Database, r.cfg.Collection, result.RunID)
	return nil
}
