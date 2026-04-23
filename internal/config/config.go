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
	Type   WorkloadType   `mapstructure:"type"`
	Custom CustomWorkload `mapstructure:"custom"`
}

// CustomWorkload percentages must sum to 100.
type CustomWorkload struct {
	Read            float64 `mapstructure:"read"`
	Insert          float64 `mapstructure:"insert"`
	Update          float64 `mapstructure:"update"`
	Delete          float64 `mapstructure:"delete"`
	Scan            float64 `mapstructure:"scan"`
	ReadModifyWrite float64 `mapstructure:"readModifyWrite"`
}

// ─── Document shape ───────────────────────────────────────────────────────────

type DocumentShapeConfig struct {
	FieldCount       int  `mapstructure:"fieldCount"`
	FieldSize        int  `mapstructure:"fieldSize"` // target bytes per string field
	NestedDocs       bool `mapstructure:"nestedDocs"`
	NestedDepth      int  `mapstructure:"nestedDepth"`
	Arrays           bool `mapstructure:"arrays"`
	ArraySize        int  `mapstructure:"arraySize"`
	UseRealisticData bool `mapstructure:"useRealisticData"` // faker vs random strings
}

// ─── Indexes ──────────────────────────────────────────────────────────────────

type IndexConfig struct {
	Field  string `mapstructure:"field"`
	Type   string `mapstructure:"type"` // asc | desc | text | geo2dsphere
	Sparse bool   `mapstructure:"sparse"`
	Unique bool   `mapstructure:"unique"`
}

// ─── Execution ────────────────────────────────────────────────────────────────

type ExecutionConfig struct {
	Mode            ExecutionMode `mapstructure:"mode"`
	Duration        string        `mapstructure:"duration"` // e.g. "5m"
	OperationCount  int64         `mapstructure:"operationCount"`
	Threads         int           `mapstructure:"threads"`
	TargetOpsPerSec int           `mapstructure:"targetOpsPerSec"` // 0 = unlimited
	Rampup          RampupConfig  `mapstructure:"rampup"`
}

type RampupConfig struct {
	InitialThreads int    `mapstructure:"initialThreads"`
	MaxThreads     int    `mapstructure:"maxThreads"`
	StepSize       int    `mapstructure:"stepSize"`
	StepDuration   string `mapstructure:"stepDuration"` // e.g. "30s"
}

func (e *ExecutionConfig) ParseDuration() (time.Duration, error) {
	return time.ParseDuration(e.Duration)
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
	DocumentCount int64 `mapstructure:"documentCount"`
	Threads       int   `mapstructure:"threads"`
}

type WarmupConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Duration string `mapstructure:"duration"`
}

// ─── Scheduling ───────────────────────────────────────────────────────────────

type ScheduleConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Cron    string `mapstructure:"cron"`
}

// ─── Results storage ──────────────────────────────────────────────────────────

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

// Load unmarshals the viper config into a Config struct.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return cfg, nil
}
