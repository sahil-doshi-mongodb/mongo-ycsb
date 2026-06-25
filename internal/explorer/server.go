package explorer

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/sahil-doshi-mongodb/mongo-ycsb/internal/models"
)

//go:embed web
var webFS embed.FS

// Options configures the explore web server.
type Options struct {
	URI         string
	Database    string
	Collection  string
	Port        int
	OpenBrowser bool
}

type server struct {
	client *mongo.Client
	coll   *mongo.Collection
	opts   Options
}

// Serve connects to the results cluster and starts the local web server.
// It blocks until the server stops or the process is interrupted.
func Serve(ctx context.Context, opts Options) error {
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(connectCtx, options.Client().
		ApplyURI(opts.URI).
		SetServerSelectionTimeout(10*time.Second))
	if err != nil {
		return fmt.Errorf("failed to connect to results cluster: %w", err)
	}
	if err := client.Ping(connectCtx, nil); err != nil {
		return fmt.Errorf("failed to reach results cluster: %w", err)
	}
	defer client.Disconnect(context.Background())

	srv := &server{
		client: client,
		coll:   client.Database(opts.Database).Collection(opts.Collection),
		opts:   opts,
	}

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("embed web dir: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	mux.HandleFunc("GET /api/runs", srv.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", srv.handleGetRun)
	mux.HandleFunc("POST /api/export/excel", srv.handleExportExcel)

	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)
	url := fmt.Sprintf("http://%s/", addr)

	fmt.Printf("🌐 mongo-ycsb explore\n")
	fmt.Printf("   Results : %s.%s\n", opts.Database, opts.Collection)
	fmt.Printf("   URL     : %s\n", url)
	fmt.Printf("   Press Ctrl+C to stop.\n")

	if opts.OpenBrowser {
		go openBrowser(url)
	}

	httpSrv := &http.Server{Addr: addr, Handler: mux}
	return httpSrv.ListenAndServe()
}

// runListItem is the lightweight projection used for the picker table.
type runListItem struct {
	RunID        string    `json:"run_id"`
	Timestamp    time.Time `json:"timestamp"`
	Tags         []string  `json:"tags"`
	Workload     string    `json:"workload"`
	Mode         string    `json:"mode"`
	Threads      int       `json:"threads"`
	Duration     string    `json:"duration"`
	Database     string    `json:"database"`
	Collection   string    `json:"collection"`
	OpsPerSec    float64   `json:"ops_per_sec"`
	TotalOps     int64     `json:"total_ops"`
	TotalErrors  int64     `json:"total_errors"`
	MongoVersion string    `json:"mongo_version"`
}

func (s *server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Exclude heavy arrays — the list only needs summary/metadata.
	projection := bson.M{
		"delta":          0,
		"system_samples": 0,
		"server_stats":   0,
		"error_samples":  0,
	}
	findOpts := options.Find().
		SetProjection(projection).
		SetSort(bson.D{{Key: "timestamp", Value: -1}})

	cur, err := s.coll.Find(ctx, bson.M{}, findOpts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	defer cur.Close(ctx)

	items := []runListItem{}
	for cur.Next(ctx) {
		var rr models.RunResult
		if err := cur.Decode(&rr); err != nil {
			continue
		}
		item := runListItem{
			RunID:       rr.RunID,
			Timestamp:   rr.Timestamp,
			Tags:        rr.Tags,
			Workload:    rr.Config.Workload,
			Mode:        rr.Config.Mode,
			Threads:     rr.Config.Threads,
			Duration:    rr.Config.Duration,
			Database:    rr.Config.Database,
			Collection:  rr.Config.Collection,
			OpsPerSec:   rr.Summary.OpsPerSec,
			TotalOps:    rr.Summary.TotalOps,
			TotalErrors: rr.Summary.TotalErrors,
		}
		if rr.ClusterInfo != nil {
			item.MongoVersion = rr.ClusterInfo.MongoVersion
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var rr models.RunResult
	err := s.coll.FindOne(ctx, bson.M{"run_id": id}).Decode(&rr)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeError(w, http.StatusNotFound, fmt.Sprintf("run %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, rr)
}

func (s *server) handleExportExcel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RunIDs []string `json:"run_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.RunIDs) == 0 {
		writeError(w, http.StatusBadRequest, "no run_ids provided")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var runs []*models.RunResult
	for _, id := range body.RunIDs {
		var rr models.RunResult
		if err := s.coll.FindOne(ctx, bson.M{"run_id": id}).Decode(&rr); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("load run %s: %v", id, err))
			return
		}
		cp := rr
		runs = append(runs, &cp)
	}

	data, err := BuildExcel(runs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("excel build failed: %v", err))
		return
	}

	filename := fmt.Sprintf("mongo-ycsb-comparison-%s.xlsx", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(data)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func openBrowser(url string) {
	time.Sleep(500 * time.Millisecond)
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
