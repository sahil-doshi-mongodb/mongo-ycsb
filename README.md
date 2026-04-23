

# mongo-ycsb

A MongoDB-native YCSB-compatible benchmarking tool written in Go. Designed to produce results directly comparable to the original [Yahoo! Cloud Serving Benchmark (YCSB)](https://github.com/brianfrankcooper/YCSB) while adding significantly better observability, result storage, scheduling, and comparison capabilities — with no JVM required.

---

## Table of Contents

- [Why mongo-ycsb](#why-mongo-ycsb)
- [Features](#features)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [CLI Reference](#cli-reference)
- [Configuration Reference](#configuration-reference)
- [Workloads](#workloads)
- [Key Distributions](#key-distributions)
- [Execution Modes](#execution-modes)
- [Result Storage & Reporting](#result-storage--reporting)
- [Comparison Mode](#comparison-mode)
- [CRON Scheduling](#cron-scheduling)
- [Replicating Original YCSB Results](#replicating-original-ycsb-results)
- [Project Structure](#project-structure)
- [Dependencies](#dependencies)

---

## Why mongo-ycsb

The original YCSB is a Java tool with broad database support but limited MongoDB-specific controls and no built-in result persistence or comparison. `mongo-ycsb` is purpose-built for MongoDB with:

| Capability | Original YCSB | mongo-ycsb |
|---|---|---|
| Zipfian / Latest / Sequential key distributions | ✅ | ✅ |
| Configurable read/write concern | ❌ | ✅ |
| Configurable read preference | ❌ | ✅ |
| Connection pool warm-up | ❌ | ✅ |
| Live console ticker (ops/sec + p99) | ❌ | ✅ |
| HDR histogram percentiles (p50→p99.999) | ✅ | ✅ |
| Delta time-series per run | ❌ | ✅ |
| System metrics (CPU/memory) | ❌ | ✅ |
| Server opcounter verification | ❌ | ✅ |
| Result storage in MongoDB | ❌ | ✅ |
| JSON / CSV / HTML reports | ❌ | ✅ |
| Comparison mode (by run ID or tag) | ❌ | ✅ |
| CRON scheduling with bounds | ❌ | ✅ |
| Time-bound execution mode | ❌ | ✅ |
| Ramp-up concurrency mode | ❌ | ✅ |
| Target throughput throttle | ✅ | ✅ |
| Variable scan length (Workload E) | ✅ | ✅ |
| Single binary — no JVM needed | ❌ | ✅ |
| Atlas native (SRV, TLS, pool tuning) | ❌ | ✅ |

---

## Features

### Workloads
- Standard YCSB workloads **A through F** out of the box
- **Custom workload** definitions via config — define your own read/insert/update/delete/scan/RMW mix
- `writeAllFields` and `readAllFields` flags to match original YCSB behaviour exactly
- Variable scan length for Workload E (configurable min/max with uniform or Zipfian distribution)

### Key Distributions
- **Zipfian** (default in original YCSB) — realistic skewed access; ~20% of keys get ~80% of traffic
- **Uniform** — equal probability for every key
- **Latest** — exponentially biased toward recently inserted keys (Workload D semantic)
- **Sequential** — rotates through keys in order

### Execution Modes
- **Time-bound** — run for a fixed duration (e.g. `5m`, `1h`)
- **Operation count** — run exactly N operations
- **Ramp-up** — gradually increase concurrency to find the saturation point
- **Target throughput** — cap ops/sec with a token-bucket rate limiter

### Data Generation
- Configurable field count, field size (exact bytes), nested documents, arrays
- Realistic data mode: names, emails, cities, dates, prices via `gofakeit`
- Random bytes mode: matches original YCSB default behaviour
- Exact field size enforcement — values are padded or truncated to exactly `fieldSize` bytes
- Zero-padded keys (e.g. `user000000000042`) to match original YCSB format exactly
- Hashed insert ordering to avoid shard hotspots

### Preload & Setup
- Bulk preload with configurable thread count (batch size 100)
- `skipIfExists` — skip preload if the collection already has data
- `--skip-preload` CLI flag for fast iteration on repeated runs
- Separate preload client (retries enabled) from benchmark client (retries disabled)
- Index creation after preload — single field, compound, text, geo2dsphere
- Warmup phase — runs workload for a configured duration, discards metrics

### Metrics & Observability
- **HDR Histograms** for accurate p50, p95, p99, p99.9, p99.99, p99.999 percentiles
- **Live console ticker** — refreshes every second with ops/sec, p50, p99, p9999, error count
- **Delta time-series** — per-second snapshots stored with every run result
- **System metrics** — CPU and memory sampled every second during the benchmark
- **Server opcounters** — captures `db.serverStatus().opcounters` before and after the run to verify equal workload distribution across clusters
- **Per-record scan latency** — normalised scan time (total ÷ records returned), matching YCSB `SCAN-LATENCY-PER-RECORD`
- **Acknowledged key counter** — keys only become available for reads after their insert is confirmed, matching YCSB's `AcknowledgedCounterGenerator`

### Result Storage
- **MongoDB collection** — full `RunResult` document with all metrics, delta, system samples, and server stats
- **Local JSON** — `./results/<run_id>.json` — always written as a fallback
- **CSV** — flat per-operation metrics file (summary only — cannot be used for comparison)
- **HTML report** — self-contained with Chart.js charts for throughput, p99 latency, CPU, memory

### Comparison Mode
- Compare any two runs **by Run ID** or **by tag** (most recent run per tag)
- Console output — side-by-side table with delta percentages
- HTML comparison report — overlaid throughput and p99 latency charts for both runs
- Loads from **MongoDB first**, falls back to **local JSON** automatically
- **CSV cannot be used for comparison** — it contains summary metrics only, not the full RunResult

### Scheduling
- CRON scheduling with a single active bound type (set `bounds.type`):
  - `unlimited` — runs forever until Ctrl+C
  - `runFor` — stops after a total wall-clock duration
  - `maxRuns` — stops after N completed runs
  - `timeWindow` — only fires between `startAt` and `stopAt` timestamps
- `dry-run` shows the next 5 trigger times, window markers, and estimated run count

---

## Requirements

- Go 1.21+
- Access to a MongoDB cluster (self-managed or Atlas)
- No JVM, no Maven, no external dependencies beyond `go mod tidy`

---

## Installation

```bash
git clone https://github.com/sahil-doshi-mongodb/mongo-ycsb.git
cd mongo-ycsb
go mod tidy
go build -o mongo-ycsb .
```

Or run directly without building:

```bash
go run main.go <command> [flags]
```

---

## Quick Start

### 1. Copy and edit the example config

```bash
cp configs/example.yaml configs/mytest.yaml
# Edit configs/mytest.yaml — set connection.uri to your MongoDB cluster
```

### 2. Validate the config

```bash
go run main.go dry-run --config configs/mytest.yaml
```

### 3. Run a benchmark

```bash
go run main.go run \
  --config configs/mytest.yaml \
  --workload A \
  --threads 50 \
  --duration 5m \
  --tags "baseline"
```

### 4. Run a second benchmark with different settings

```bash
go run main.go run \
  --config configs/mytest.yaml \
  --workload A \
  --threads 50 \
  --duration 5m \
  --skip-preload \
  --tags "after-index"
```

### 5. Compare the two runs

```bash
go run main.go compare \
  --config configs/mytest.yaml \
  --tag-a "baseline" \
  --tag-b "after-index" \
  --output both
```

---

## CLI Reference

### `run` — Execute a benchmark

```bash
mongo-ycsb run --config <path> [flags]
```

| Flag | Description |
|---|---|
| `--config` | Path to YAML config file (required) |
| `--workload` | Workload type: A, B, C, D, E, F, or custom |
| `--threads` | Number of concurrent goroutines |
| `--duration` | Run duration e.g. `30s`, `5m`, `1h` (sets mode=time) |
| `--ops` | Total operation count (sets mode=ops) |
| `--uri` | MongoDB connection URI (overrides config) |
| `--database` | Database name (overrides config) |
| `--collection` | Collection name (overrides config) |
| `--tags` | Comma-separated tags for this run e.g. `baseline,v8,m40` |
| `--skip-preload` | Skip preload and use existing collection data |

### `dry-run` — Validate config without running

```bash
mongo-ycsb dry-run --config <path>
```

Validates the full config and prints the full benchmark plan — never touches MongoDB. When scheduling is enabled, shows the next 5 trigger times and estimated run count.

### `compare` — Diff two benchmark runs

```bash
# By run ID
mongo-ycsb compare --config <path> <run-id-1> <run-id-2>

# By tag (most recent run per tag)
mongo-ycsb compare --config <path> --tag-a baseline --tag-b after-index

# With HTML output
mongo-ycsb compare --config <path> --tag-a v7 --tag-b v8 --output both
```

| Flag | Description |
|---|---|
| `--tag-a` | Tag for Run A |
| `--tag-b` | Tag for Run B |
| `--output` | `console` (default) \| `html` \| `both` |
| `--html-path` | Directory for HTML comparison report (default `./reports`) |

### `schedule` — Run benchmarks on a CRON schedule

```bash
mongo-ycsb schedule --config <path> [--skip-preload]
```

Reads schedule configuration from the config file. Blocks until the configured bound is satisfied or the process is interrupted.

### `report` — Generate an HTML report for a completed run

```bash
mongo-ycsb report --config <path> <run-id>
```

---

## Configuration Reference

### Connection

```yaml
connection:
  uri: "mongodb+srv://user:pass@cluster.mongodb.net/"
  database: "ycsb"
  collection: "usertable"
  readPreference: "primary"        # primary | primaryPreferred | secondary | secondaryPreferred | nearest
  readConcern: "local"             # local | majority | linearizable | available
  writeConcern: "majority"         # majority | w:1 | w:0
  connectionPoolSize: 100
  timeoutMs: 30000
```

### Workload

```yaml
workload:
  type: "A"                        # A | B | C | D | E | F | custom
  writeAllFields: false            # false = update 1 field (YCSB default)
  readAllFields: true              # true = read full document (YCSB default)

  # Only used when type: custom — must sum to 100
  custom:
    read: 50
    insert: 0
    update: 50
    delete: 0
    scan: 0
    readModifyWrite: 0

  # Scan length — applies to Workload E and custom workloads with scan > 0
  scan:
    minLength: 1                   # YCSB default
    maxLength: 1000                # YCSB default; Workload E uses 100
    distribution: "uniform"        # uniform | zipfian
```

### Standard Workload Reference

| Workload | Read % | Insert % | Update % | Scan % | RMW % | Semantic |
|---|---|---|---|---|---|---|
| **A** | 50 | — | 50 | — | — | Session store — heavy update |
| **B** | 95 | — | 5 | — | — | Photo tagging — mostly reads |
| **C** | 100 | — | — | — | — | User profile cache — read only |
| **D** | 95 | 5 | — | — | — | Read latest — uses Latest distribution |
| **E** | — | 5 | — | 95 | — | Short ranges — variable scan length |
| **F** | 50 | — | — | — | 50 | Read-modify-write |

### Document Shape

```yaml
documentShape:
  fieldCount: 10                   # number of fields per document
  fieldSize: 100                   # exact bytes per field value
  nestedDocs: false                # include a nested sub-document
  nestedDepth: 2
  arrays: false                    # include an array field
  arraySize: 5
  useRealisticData: false          # false = random bytes (YCSB default)
                                   # true = names, emails, cities via gofakeit
```

### Indexes

Original YCSB creates **no secondary indexes** — all queries run against the default `_id` index only. To match this behaviour exactly:

```yaml
indexes: []
```

The `dry-run` command confirms this:
```
Indexes : none — only default _id index
          ↳ matches original YCSB behaviour (no secondary indexes)
```

To benchmark with secondary index overhead (e.g. before vs after adding an index):

```yaml
# Single field index
indexes:
  - field: "field0"
    type: "asc"                    # asc | desc | text | geo2dsphere
    sparse: false
    unique: false

# Compound index
indexes:
  - fields:
      - field: "field0"
        type: "asc"
      - field: "field1"
        type: "desc"
    sparse: false
    unique: false
```

Indexes are always created **after preload** so the collection `Drop()` during preload does not wipe them.

### Execution

```yaml
execution:
  mode: "time"                     # time | ops | rampup
  duration: "5m"                   # used when mode: time
  operationCount: 1000000          # used when mode: ops
  threads: 50
  targetOpsPerSec: 0               # 0 = unlimited; >0 = token-bucket throttle

  # Key space & distribution
  keyDistribution: "zipfian"       # uniform | zipfian | latest | sequential
  zipfianConstant: 0.99            # YCSB default — controls skew (0 < θ < 1)
  recordCount: 0                   # 0 = use preload.documentCount
  keyPrefix: "user"                # YCSB default
  keyZeroPadding: 0                # 0 = no padding; 12 = user000000000042
  insertOrdering: "ordered"        # ordered | hashed (hashed avoids shard hotspots)

  # Ramp-up — only used when mode: rampup
  rampup:
    initialThreads: 1
    maxThreads: 100
    stepSize: 10
    stepDuration: "30s"
```

### Phases

```yaml
phases:
  preload:
    enabled: true
    skipIfExists: false            # skip if collection already has data
    documentCount: 100000
    threads: 20
  warmup:
    enabled: true
    duration: "30s"               # run workload, discard metrics
```

### Results Storage

```yaml
results:
  mongodb:
    enabled: true
    uri: "mongodb+srv://..."       # can be same or separate cluster
    database: "ycsb_results"
    collection: "runs"
  local:
    enabled: true
    path: "./results"             # writes <run_id>.json
  tags:
    - "baseline"
    - "v8"
    - "m40"
```

#### `results.tags` — What They Are and How to Use Them

Tags are free-form string labels attached to every run produced by this config.

**Purpose 1 — Identification**: describe what makes this run different from others. Tags appear in every result: MongoDB document, JSON file, HTML report. When you look at a result later, tags tell you what config produced it without reading the full config snapshot.

**Purpose 2 — Tag-based comparison**: find runs without remembering run IDs.
```bash
go run main.go compare --tag-a "before-index" --tag-b "after-index"
```
Finds the most recent run with each tag automatically.

**Purpose 3 — MongoDB filtering**: query your results collection directly.
```js
db.runs.find({ tags: { $all: ["v8", "zipfian", "workload-a"] } })
db.runs.find({ tags: "v7" }).sort({ timestamp: -1 })
```

**Good tags** describe: version (`v7`, `v8`), cluster tier (`m40`, `m60`), workload (`workload-a`), distribution (`zipfian`, `uniform`), write concern (`majority`, `w1`), and state (`baseline`, `before-index`, `after-index`).

**Limitation**: `--tag-a`/`--tag-b` match on a single tag string and pick the most recent run with that tag. If multiple runs share the same tag, use run IDs directly for precise targeting.

### Reporting

```yaml
reporting:
  console:
    enabled: true
    refreshIntervalMs: 1000       # live ticker refresh interval
  html:
    enabled: true
    outputPath: "./reports"       # writes <run_id>.html
  csv:
    enabled: true
    outputPath: "./results"       # writes <run_id>.csv
```

### Scheduling

```yaml
schedule:
  enabled: false
  cron: "0 * * * *"              # standard 5-field cron expression

  bounds:
    # Set type to exactly ONE of: unlimited | runFor | maxRuns | timeWindow
    # Only populate the fields for the type you choose.
    type: "unlimited"

    # Used when type: runFor — total duration from start
    runFor: ""                    # e.g. "600s", "2h"

    # Used when type: maxRuns — stop after N completed runs
    maxRuns: 0                    # e.g. 10

    # Used when type: timeWindow — both required
    startAt: ""                   # RFC3339 e.g. "2026-05-01T00:00:00Z"
    stopAt:  ""                   # RFC3339 e.g. "2026-05-07T23:59:59Z"
```

| Bound Type | Stops When |
|---|---|
| `unlimited` | Ctrl+C only |
| `runFor` | Total wall-clock duration elapsed from start |
| `maxRuns` | N runs successfully completed |
| `timeWindow` | A trigger fires after `stopAt` timestamp |

Only one `bounds.type` is active per scheduler. Setting fields for a different type than the one selected is flagged as a validation error during `dry-run`.

---

## Key Distributions

### Zipfian (recommended for realistic benchmarks)

Matches the original YCSB default. ~20% of keys receive ~80% of traffic. Stresses WiredTiger cache realistically and produces p99/p999 values representative of real workloads.

```yaml
execution:
  keyDistribution: "zipfian"
  zipfianConstant: 0.99           # higher = more skewed; YCSB default
```

### Uniform

Every key has equal probability. Produces optimistic (lower) latency numbers because the cache hit rate is much higher. Useful as a best-case baseline.

```yaml
execution:
  keyDistribution: "uniform"
```

### Latest

Used by Workload D. Exponentially biased toward recently inserted keys — simulates social feeds, activity streams, order queues.

```yaml
execution:
  keyDistribution: "latest"
```

### Sequential

Rotates through all existing keys in order. Useful for simulating batch processing or sequential scan patterns.

```yaml
execution:
  keyDistribution: "sequential"
```

---

## Execution Modes

### Time-bound

```bash
mongo-ycsb run --config config.yaml --duration 5m
```

```yaml
execution:
  mode: "time"
  duration: "5m"
```

### Operation count

```bash
mongo-ycsb run --config config.yaml --ops 10000000
```

```yaml
execution:
  mode: "ops"
  operationCount: 10000000
```

### Ramp-up

Gradually increases concurrency from `initialThreads` to `maxThreads` in steps. Each step runs for `stepDuration`. Use this to find the saturation point of a cluster.

```yaml
execution:
  mode: "rampup"
  rampup:
    initialThreads: 10
    maxThreads: 200
    stepSize: 10
    stepDuration: "30s"
```

### Target throughput

Caps the benchmark to a specific ops/sec rate using a token-bucket limiter. Useful for controlled comparisons where throughput must be equal between runs.

```yaml
execution:
  targetOpsPerSec: 1000           # cap at 1000 ops/sec
```

---

## Result Storage & Reporting

Every completed benchmark run produces:

### Console summary

```
✅ Benchmark Complete
   Run ID           : a3b8c73c-11a4-4337-9e4d-7b0187565506
   Duration         : 300.00s
   Total Ops        : 150420
   Errors           : 0
   Throughput       : 501 ops/sec
   Key Distribution : zipfian

   Operation           Count   Errors  Mean ms  p50 ms  p99 ms  p999 ms  p9999 ms  p99999 ms
   read                75210        0    98.32   87.55  310.40   892.10   1420.33    2048.00
   update              75210        0   102.14   91.20  320.18   910.22   1450.00    2100.00

   Avg CPU  : 12.4%
   Peak Mem : 8420 MB

   Server Opcounters (delta during benchmark):
                 insert=0  query=75210  update=75210  delete=0
```

### MongoDB document

Full `RunResult` document stored in `ycsb_results.runs` containing all metrics, delta time-series, system samples, and server opcounters.

### JSON file

`./results/<run_id>.json` — complete run result for offline analysis and comparison.

### CSV file

`./results/<run_id>.csv` — flat per-operation metrics, one row per operation type. Useful for importing into Excel or Google Sheets. **Cannot be used for comparison mode** — use JSON or MongoDB for that.

### HTML report

`./reports/<run_id>.html` — self-contained interactive report with four Chart.js charts:
- Throughput over time (ops/sec)
- p99 latency over time (ms)
- CPU usage (%)
- Memory usage (MB)

---

## Comparison Mode

Compare two runs and produce a side-by-side diff.

```bash
# By run ID
mongo-ycsb compare --config config.yaml \
  a3b8c73c-11a4-4337-9e4d-7b0187565506 \
  cad74787-b79f-4900-a2ca-c7103b5079ae

# By tag — uses the most recent run per tag
mongo-ycsb compare --config config.yaml \
  --tag-a "before-index" \
  --tag-b "after-index" \
  --output both
```

Console output shows side-by-side latency percentiles with delta percentages. HTML output generates `./reports/compare_<runA>_vs_<runB>.html` with overlaid Chart.js charts.

Runs are loaded from **MongoDB first**, falling back to **local JSON** automatically if MongoDB is unavailable or disabled. **CSV files cannot be used for comparison** — they contain summary metrics only, not the full RunResult needed for a diff.

---

## CRON Scheduling

Run benchmarks automatically on a schedule. Configure the trigger expression and choose exactly one bound type:

```yaml
schedule:
  enabled: true
  cron: "0 * * * *"              # trigger every hour

  bounds:
    type: "timeWindow"            # fires only within this window
    startAt: "2026-05-01T00:00:00Z"
    stopAt:  "2026-05-03T00:00:00Z"
```

```bash
mongo-ycsb schedule --config config.yaml --skip-preload
```

The `dry-run` command shows a full schedule preview before you commit to running:

```
   ⏰ CRON Schedule
      Expression : "0 * * * *"
      Bound Type : timeWindow
      Start At   : 2026-05-01T00:00:00Z
      Stop At    : 2026-05-03T00:00:00Z

      Next 5 trigger times:
         1. 2026-04-24 19:00:00 UTC  ⏭️  (before window — will be skipped)
         2. 2026-05-01 00:00:00 UTC  ✅
         3. 2026-05-01 01:00:00 UTC  ✅
         4. 2026-05-01 02:00:00 UTC  ✅
         5. 2026-05-01 03:00:00 UTC  ✅

      📊 Estimated runs in window: 48
```

Each triggered run gets its own `run_id` and is stored independently, making it easy to track performance over time.

---

## Replicating Original YCSB Results

To produce results directly comparable to original YCSB:

```yaml
workload:
  type: "A"
  writeAllFields: false           # YCSB default
  readAllFields: true             # YCSB default

documentShape:
  fieldCount: 10                  # YCSB default
  fieldSize: 100                  # set to match -p fieldlength=N
  useRealisticData: false         # YCSB uses random bytes

indexes: []                       # YCSB creates no secondary indexes

execution:
  keyDistribution: "zipfian"      # YCSB default
  zipfianConstant: 0.99           # YCSB default
  recordCount: 1000000            # set to match -p recordcount=N
  keyZeroPadding: 0               # YCSB modern default
```

---

## Project Structure

```
mongo-ycsb/
├── main.go
├── cmd/
│   ├── root.go          # CLI root, config initialisation
│   ├── run.go           # run command
│   ├── dryrun.go        # dry-run command
│   ├── compare.go       # compare command
│   ├── schedule.go      # schedule command
│   └── report.go        # report command
├── internal/
│   ├── config/
│   │   ├── config.go    # full config schema
│   │   └── validator.go # pre-flight validation
│   ├── db/
│   │   └── client.go    # benchmark + preload MongoDB clients
│   ├── datagen/
│   │   └── generator.go # document + key generation, all distributions
│   ├── distribution/
│   │   └── zipfian.go   # ScrambledZipfian + Latest distribution
│   ├── workloads/
│   │   └── workloads.go # standard workloads A–F + custom selector
│   ├── loader/
│   │   └── loader.go    # preload phase + index creation
│   ├── worker/
│   │   ├── worker.go    # individual operation executor
│   │   └── pool.go      # goroutine pool + rate limiter
│   ├── metrics/
│   │   ├── metrics.go   # HDR histogram recorder
│   │   ├── ticker.go    # live console ticker
│   │   └── system.go    # CPU/memory sampler
│   ├── models/
│   │   └── run.go       # RunResult document schema
│   ├── orchestrator/
│   │   └── orchestrator.go  # coordinates all phases
│   ├── reporter/
│   │   ├── mongodb.go   # MongoDB result storage
│   │   ├── local.go     # JSON + CSV file output
│   │   └── html.go      # HTML report generation
│   ├── comparer/
│   │   ├── comparer.go  # run diff logic
│   │   └── html.go      # HTML comparison report
│   └── scheduler/
│       └── scheduler.go # CRON scheduler with bounds
└── configs/
    ├── example.yaml     # fully annotated example config
    └── zepto.yaml       # Zepto benchmark replication config
```

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `go.mongodb.org/mongo-driver` | v1.17.9 | Official MongoDB Go driver |
| `github.com/spf13/cobra` | v1.8.0 | CLI framework |
| `github.com/spf13/viper` | v1.18.2 | YAML config + env var overrides |
| `github.com/HdrHistogram/hdrhistogram-go` | v1.2.0 | Accurate latency percentiles |
| `github.com/brianvoe/gofakeit/v6` | v6.28.0 | Realistic synthetic data generation |
| `github.com/robfig/cron/v3` | v3.0.1 | CRON scheduler |
| `github.com/shirou/gopsutil/v3` | v3.24.5 | CPU and memory sampling |
| `github.com/google/uuid` | v1.4.0 | Run ID generation |
| `go.uber.org/zap` | v1.21.0 | Structured logging |
