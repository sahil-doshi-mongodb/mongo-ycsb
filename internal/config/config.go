package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// ─── Enum types ───────────────────────────────────────────────────────────────

type ExecutionMode string

const (
	ModeTime   ExecutionMode = "time"
	ModeOps    ExecutionMode = "ops"
	ModeRampup ExecutionMode = "rampup"
)

type WorkloadType string

const (
	WorkloadA      WorkloadType = "A"
	WorkloadB      WorkloadType = "B"
	WorkloadC      WorkloadType = "C"
	WorkloadD      WorkloadType = "D"
	WorkloadE      WorkloadType = "E"
	WorkloadF      WorkloadType = "F"
	WorkloadCustom WorkloadType = "custom"
)

// ─── Root config ──────────────────────────────────────────────────────────────

type Config struct {
	Connection    ConnectionConfig    `mapstructure:"connection"`
	Workload      WorkloadConfig      `mapstructure:"workload"`
	DocumentShape DocumentShapeConfig `mapstructure:"documentShape"`
	Indexes       []IndexConfig       `mapstructure:"indexes"`
	Execution     ExecutionConfig     `mapstructure:"execution"`
	Phases        PhasesConfig        `mapstructure:"phases"`
	Schedule      ScheduleConfig      `mapstructure:"schedule"`
	Results       ResultsConfig       `mapstructure:"results"`
	Reporting     ReportingConfig     `mapstructure:"reporting"`
}

// ─── Connection ───────────────────────────────────────────────────────────────

type ConnectionConfig struct {
	URI                string `mapstructure:"uri"`
	Database           string `mapstructure:"database"`
	Collection         string `mapstructure:"collection"`
	ReadPreference     string `mapstructure:"readPreference"`
	ReadConcern        string `mapstructure:"readConcern"`
	WriteConcern       string `mapstructure:"writeConcern"`
	ConnectionPoolSize uint64 `mapstructure:"connectionPoolSize"`
	TimeoutMs          int64  `mapstructure:"timeoutMs"`
}

// ─── Workload ─────────────────────────────────────────────────────────────────

type WorkloadConfig struct {
	Type           WorkloadType   `mapstructure:"type"`
	Custom         CustomWorkload `mapstructure:"custom"`
	WriteAllFields bool           `mapstructure:"writeAllFields"` // false = update 1 field (YCSB default)
	ReadAllFields  bool           `mapstructure:"readAllFields"`  // true = read full doc (YCSB default)
	Scan           ScanConfig     `mapstructure:"scan"`
}

type CustomWorkload struct {
	Read            float64 `mapstructure:"read"`
	Insert          float64 `mapstructure:"insert"`
	Update          float64 `mapstructure:"update"`
	Delete          float64 `mapstructure:"delete"`
	Scan            float64 `mapstructure:"scan"`
	ReadModifyWrite float64 `mapstructure:"readModifyWrite"`
}

// ScanConfig controls scan length distribution — mirrors YCSB Workload E params.
type ScanConfig struct {
	MinLength    int    `mapstructure:"minLength"`    // default 1
	MaxLength    int    `mapstructure:"maxLength"`    // default 1000; Workload E uses 100
	Distribution string `mapstructure:"distribution"` // uniform | zipfian (default uniform)
}

// ─── Document shape ───────────────────────────────────────────────────────────

type DocumentShapeConfig struct {
	FieldCount       int  `mapstructure:"fieldCount"`
	FieldSize        int  `mapstructure:"fieldSize"` // exact bytes per field value
	NestedDocs       bool `mapstructure:"nestedDocs"`
	NestedDepth      int  `mapstructure:"nestedDepth"`
	Arrays           bool `mapstructure:"arrays"`
	ArraySize        int  `mapstructure:"arraySize"`
	UseRealisticData bool `mapstructure:"useRealisticData"` // false = random bytes (YCSB default)
}

// ─── Indexes ──────────────────────────────────────────────────────────────────

type IndexConfig struct {
	Field  string       `mapstructure:"field"`
	Type   string       `mapstructure:"type"`
	Fields []IndexField `mapstructure:"fields"` // compound index
	Sparse bool         `mapstructure:"sparse"`
	Unique bool         `mapstructure:"unique"`
}

type IndexField struct {
	Field string `mapstructure:"field"`
	Type  string `mapstructure:"type"`
}

// ─── Execution ────────────────────────────────────────────────────────────────

type ExecutionConfig struct {
	Mode            ExecutionMode `mapstructure:"mode"`
	Duration        string        `mapstructure:"duration"`
	OperationCount  int64         `mapstructure:"operationCount"`
	Threads         int           `mapstructure:"threads"`
	TargetOpsPerSec int           `mapstructure:"targetOpsPerSec"` // 0 = unlimited
	Rampup          RampupConfig  `mapstructure:"rampup"`

	// Key space & distribution — mirrors YCSB -p properties
	RecordCount     int64   `mapstructure:"recordCount"`     // key space size; 0 = use preload count
	KeyDistribution string  `mapstructure:"keyDistribution"` // uniform | zipfian | latest | sequential
	ZipfianConstant float64 `mapstructure:"zipfianConstant"` // default 0.99
	KeyPrefix       string  `mapstructure:"keyPrefix"`       // default "user"
	KeyZeroPadding  int     `mapstructure:"keyZeroPadding"`  // 0 = no padding; YCSB default = 0
	InsertOrdering  string  `mapstructure:"insertOrdering"`  // ordered | hashed (default ordered)
}

func (e *ExecutionConfig) ParseDuration() (time.Duration, error) {
	return time.ParseDuration(e.Duration)
}

// EffectiveKeyPrefix returns the configured prefix or the default "user".
func (e *ExecutionConfig) EffectiveKeyPrefix() string {
	if e.KeyPrefix == "" {
		return "user"
	}
	return e.KeyPrefix
}

// EffectiveZipfianConstant returns the configured constant or the YCSB default.
func (e *ExecutionConfig) EffectiveZipfianConstant() float64 {
	if e.ZipfianConstant == 0 {
		return 0.99
	}
	return e.ZipfianConstant
}

type RampupConfig struct {
	InitialThreads int    `mapstructure:"initialThreads"`
	MaxThreads     int    `mapstructure:"maxThreads"`
	StepSize       int    `mapstructure:"stepSize"`
	StepDuration   string `mapstructure:"stepDuration"`
}

func (r *RampupConfig) ParseStepDuration() (time.Duration, error) {
	return time.ParseDuration(r.StepDuration)
}

// ─── Phases ───────────────────────────────────────────────────────────────────

type PhasesConfig struct {
	Preload PreloadConfig `mapstructure:"preload"`
	Warmup  WarmupConfig  `mapstructure:"warmup"`
}

type PreloadConfig struct {
	Enabled       bool  `mapstructure:"enabled"`
	SkipIfExists  bool  `mapstructure:"skipIfExists"`
	DocumentCount int64 `mapstructure:"documentCount"`
	Threads       int   `mapstructure:"threads"`
}

type WarmupConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Duration string `mapstructure:"duration"`
}

// ─── Scheduling ───────────────────────────────────────────────────────────────

// ─── Scheduling ───────────────────────────────────────────────────────────────

type ScheduleConfig struct {
	Enabled bool         `mapstructure:"enabled"`
	Cron    string       `mapstructure:"cron"` // standard 5-field cron expression
	Bounds  BoundsConfig `mapstructure:"bounds"`
}

// BoundsConfig defines when the scheduler stops.
// Set bounds.type to exactly ONE of: unlimited | runFor | maxRuns | timeWindow
type BoundsConfig struct {
	// Type controls which bound is active. Only one is used at a time.
	//   unlimited  — runs forever until manually stopped (Ctrl+C)
	//   runFor     — stops after a total wall-clock duration from start
	//   maxRuns    — stops after N completed runs
	//   timeWindow — only fires between startAt and stopAt timestamps
	Type string `mapstructure:"type"`

	// Used when type: runFor — e.g. "600s", "2h", "30m"
	RunFor string `mapstructure:"runFor"`

	// Used when type: maxRuns — must be > 0
	MaxRuns int `mapstructure:"maxRuns"`

	// Used when type: timeWindow — both must be valid RFC3339 timestamps
	StartAt string `mapstructure:"startAt"`
	StopAt  string `mapstructure:"stopAt"`
}

// ParseRunFor parses the RunFor field as a time.Duration.
func (b *BoundsConfig) ParseRunFor() (time.Duration, error) {
	if b.RunFor == "" {
		return 0, nil
	}
	return time.ParseDuration(b.RunFor)
}

// ParseStartAt parses StartAt as time.Time.
func (b *BoundsConfig) ParseStartAt() (time.Time, error) {
	if b.StartAt == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, b.StartAt)
}

// ParseStopAt parses StopAt as time.Time.
func (b *BoundsConfig) ParseStopAt() (time.Time, error) {
	if b.StopAt == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, b.StopAt)
}

// ─── Results ──────────────────────────────────────────────────────────────────

type ResultsConfig struct {
	MongoDB MongoResultsConfig `mapstructure:"mongodb"`
	Local   LocalResultsConfig `mapstructure:"local"`
	Tags    []string           `mapstructure:"tags"`
}

type MongoResultsConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	URI        string `mapstructure:"uri"`
	Database   string `mapstructure:"database"`
	Collection string `mapstructure:"collection"`
}

type LocalResultsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

// ─── Reporting ────────────────────────────────────────────────────────────────

type ReportingConfig struct {
	Console ConsoleConfig `mapstructure:"console"`
	HTML    HTMLConfig    `mapstructure:"html"`
	CSV     CSVConfig     `mapstructure:"csv"`
}

type ConsoleConfig struct {
	Enabled           bool `mapstructure:"enabled"`
	RefreshIntervalMs int  `mapstructure:"refreshIntervalMs"`
}

type HTMLConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	OutputPath string `mapstructure:"outputPath"`
}

type CSVConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	OutputPath string `mapstructure:"outputPath"`
}

// ─── Load ─────────────────────────────────────────────────────────────────────

func Load() (*Config, error) {
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}
