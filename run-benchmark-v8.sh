#!/bin/bash
# =============================================================
# run-benchmark-v8.sh
# Zepto MongoDB Benchmark Coordinator — v8 Cluster
#
# Run this on EC2 Instance 2 (connected to v8 cluster).
# Run run-benchmark-v7.sh simultaneously on EC2 Instance 1.
#
# BEFORE RUNNING:
#   1. Fill in connection.uri in all 6 files under configs/zepto-v8/
#   2. Fill in results.mongodb.uri in all 6 files under configs/zepto-v8/
#   3. Set INITIAL_TIER and SCALED_TIER below to match your Atlas clusters
#   4. Set TEST_MODE="true" first to validate the script (runs in ~10 min)
#   5. Set TEST_MODE="false" for the real benchmark
#
# USAGE:
#   chmod +x run-benchmark-v8.sh
#   tmux new-session -s benchmark
#   ./run-benchmark-v8.sh
#
# TO REATTACH IF SSH DROPS:
#   tmux attach-session -t benchmark
# =============================================================

set -euo pipefail

# =============================================================
# CONFIGURATION — EDIT THESE BEFORE RUNNING
# =============================================================
CLUSTER_VERSION="v8"
INITIAL_TIER="M40"
SCALED_TIER="M60"
CONFIGS="./configs/zepto-v8"
BINARY="go run main.go"
TEST_MODE="false"
# =============================================================

# ── Derived settings ──────────────────────────────────────────
LOG_FILE="benchmark-${CLUSTER_VERSION}-$(date +%Y%m%d-%H%M%S).log"
TEMP_DIR="/tmp/zepto-benchmark-$$"
mkdir -p "$TEMP_DIR"

if [ "$TEST_MODE" = "true" ]; then
    RUN_DURATION="30s"
    RUN_THREADS=10
    REST_SECONDS=10
    VERIFY_WAIT=5
    WARMUP_DISPLAY="10s"
    echo ""
    echo "  ⚠️  TEST MODE ENABLED"
    echo "  Workloads will run for 30s with 10 threads."
    echo "  Warmup is shortened to 10s."
    echo "  Rest periods are 10 seconds."
    echo "  Use this to validate the script before the real run."
    echo ""
else
    RUN_DURATION="1h"
    RUN_THREADS=512
    REST_SECONDS=3600
    VERIFY_WAIT=30
    WARMUP_DISPLAY="5m"
fi

# ── Colour codes ──────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# ── Logging helpers ───────────────────────────────────────────
log() {
    local msg="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    echo -e "${BLUE}${msg}${NC}" | tee -a "$LOG_FILE"
}

log_success() {
    local msg="[$(date '+%Y-%m-%d %H:%M:%S')] ✅ $1"
    echo -e "${GREEN}${msg}${NC}" | tee -a "$LOG_FILE"
}

log_warn() {
    local msg="[$(date '+%Y-%m-%d %H:%M:%S')] ⚠️  $1"
    echo -e "${YELLOW}${msg}${NC}" | tee -a "$LOG_FILE"
}

log_section() {
    echo "" | tee -a "$LOG_FILE"
    echo -e "${CYAN}============================================================${NC}" | tee -a "$LOG_FILE"
    echo -e "${CYAN}  $1${NC}" | tee -a "$LOG_FILE"
    echo -e "${CYAN}============================================================${NC}" | tee -a "$LOG_FILE"
}

log_error() {
    local msg="[$(date '+%Y-%m-%d %H:%M:%S')] ❌ $1"
    echo -e "${RED}${msg}${NC}" | tee -a "$LOG_FILE"
    echo -e "${RED}Script aborted. Check log: ${LOG_FILE}${NC}"
    cleanup
    exit 1
}

cleanup() {
    rm -rf "$TEMP_DIR"
}
trap cleanup EXIT

# ── Rest timer ────────────────────────────────────────────────
rest() {
    local seconds=$1
    local label=$2
    log "REST: ${label} — ${seconds}s ($(( seconds / 60 )) min)"
    local end=$(( $(date +%s) + seconds ))
    while [ $(date +%s) -lt $end ]; do
        local rem=$(( end - $(date +%s) ))
        printf "\r  ⏳ Rest remaining: %02dh %02dm %02ds  " \
            $(( rem / 3600 )) $(( (rem % 3600) / 60 )) $(( rem % 60 ))
        sleep 5
    done
    printf "\r                                          \r"
    log_success "Rest complete — ${label}"
}

# ── Tag injection and warmup patch ────────────────────────────
# Creates a temp copy of a config with tier/phase tags uncommented.
# In TEST_MODE also shortens warmup from 5m to 10s.
# The original config file is NEVER modified.
patch_config() {
    local source_config=$1
    local tier=$2
    local phase=$3
    local output_file="$TEMP_DIR/$(basename $source_config .yaml)-${tier}-${phase}.yaml"

    if [ "$TEST_MODE" = "true" ]; then
        sed \
            -e "s/^    # - \"${tier}\".*/    - \"${tier}\"/" \
            -e "s/^    # - \"${phase}\".*/    - \"${phase}\"/" \
            -e "s/duration: \"5m\"/duration: \"10s\"/" \
            "$source_config" > "$output_file"
    else
        sed \
            -e "s/^    # - \"${tier}\".*/    - \"${tier}\"/" \
            -e "s/^    # - \"${phase}\".*/    - \"${phase}\"/" \
            "$source_config" > "$output_file"
    fi

    echo "$output_file"
}

# ── Make a patched verify config (warmup only, no tag changes) ─
make_verify_config() {
    local source_config=$1
    local output_file="$TEMP_DIR/verify-$(basename $source_config)"

    if [ "$TEST_MODE" = "true" ]; then
        sed -e "s/duration: \"5m\"/duration: \"10s\"/" \
            "$source_config" > "$output_file"
    else
        cp "$source_config" "$output_file"
    fi

    echo "$output_file"
}

# ── Run a single workload ─────────────────────────────────────
run_workload() {
    local config_file=$1
    local workload_label=$2
    local tier=$3
    local phase=$4

    local patched
    patched=$(patch_config "$config_file" "$tier" "$phase")

    log "▶  Starting: ${workload_label} | ${CLUSTER_VERSION} | ${tier} | ${phase}"
    log "   Duration : ${RUN_DURATION} (+ ${WARMUP_DISPLAY} warmup)"
    log "   Threads  : ${RUN_THREADS}"
    log "   Start    : $(date '+%Y-%m-%d %H:%M:%S')"

    $BINARY run \
        --config "$patched" \
        --duration "$RUN_DURATION" \
        --threads "$RUN_THREADS" \
        --skip-preload \
        2>&1 | tee -a "$LOG_FILE" || log_error "${workload_label} failed"

    log_success "Completed: ${workload_label} | ${tier} | ${phase} | $(date '+%Y-%m-%d %H:%M:%S')"
}

# ── Run all 5 workloads for a given phase and tier ────────────
run_phase() {
    local tier=$1
    local phase=$2

    for workload in a b c d e; do
        run_workload \
            "${CONFIGS}/zepto-run-workload-${workload}.yaml" \
            "workload-${workload}" \
            "$tier" \
            "$phase"

        if [ "$workload" != "e" ]; then
            rest $REST_SECONDS "after workload-${workload} (${phase}/${tier})"
        fi
    done
}

# =============================================================
# PRE-FLIGHT CHECKS
# =============================================================
log_section "Pre-Flight Checks | ${CLUSTER_VERSION}"
log "Binary     : $BINARY"
log "Config dir : $CONFIGS"
log "Log file   : $LOG_FILE"
log "Test mode  : $TEST_MODE"

[ -f "main.go" ] || log_error "main.go not found. Run this script from the ~/mongo-ycsb directory."
[ -d "$CONFIGS" ] || log_error "Config directory not found: $CONFIGS"
[ -f "${CONFIGS}/zepto-load.yaml" ] || log_error "Missing: ${CONFIGS}/zepto-load.yaml"

for workload in a b c d e; do
    cfg="${CONFIGS}/zepto-run-workload-${workload}.yaml"
    [ -f "$cfg" ] || log_error "Missing: $cfg"
done

log "All config files present ✓"

log "Checking URIs are filled in..."
for workload in a b c d e; do
    cfg="${CONFIGS}/zepto-run-workload-${workload}.yaml"
    uri_line=$(grep "^  uri:" "$cfg" | head -1)
    if echo "$uri_line" | grep -q 'uri: ""'; then
        log_error "connection.uri is empty in $cfg — fill in the Atlas connection string before running"
    fi
    results_uri=$(grep "uri:" "$cfg" | tail -1)
    if echo "$results_uri" | grep -q 'uri: ""'; then
        log_error "results.mongodb.uri is empty in $cfg — fill in the results cluster connection string before running"
    fi
done

uri_line=$(grep "^  uri:" "${CONFIGS}/zepto-load.yaml" | head -1)
if echo "$uri_line" | grep -q 'uri: ""'; then
    log_error "connection.uri is empty in ${CONFIGS}/zepto-load.yaml — fill in the Atlas connection string before running"
fi

log_success "URIs present in all config files"

log "Validating config files..."
for workload in a b c d e; do
    $BINARY dry-run --config "${CONFIGS}/zepto-run-workload-${workload}.yaml" \
        >> "$LOG_FILE" 2>&1 || log_error "Config validation failed for workload-${workload}. Check: ${CONFIGS}/zepto-run-workload-${workload}.yaml"
done
$BINARY dry-run --config "${CONFIGS}/zepto-load.yaml" \
    >> "$LOG_FILE" 2>&1 || log_error "Config validation failed for load config"

log_success "All config files valid"

log "Verifying tag placeholders in workload configs..."
for workload in a b c d e; do
    cfg="${CONFIGS}/zepto-run-workload-${workload}.yaml"
    grep -q "# - \"${INITIAL_TIER}\"" "$cfg" || log_error "Tag placeholder '# - \"${INITIAL_TIER}\"' not found in $cfg"
    grep -q "# - \"${SCALED_TIER}\"" "$cfg"  || log_error "Tag placeholder '# - \"${SCALED_TIER}\"' not found in $cfg"
    grep -q "# - \"phase-1\"" "$cfg"          || log_error "Tag placeholder '# - \"phase-1\"' not found in $cfg"
    grep -q "# - \"phase-2\"" "$cfg"          || log_error "Tag placeholder '# - \"phase-2\"' not found in $cfg"
done
log_success "Tag placeholders verified"

echo ""
echo "  Pre-flight complete. Starting benchmark in 5 seconds."
echo "  Press Ctrl+C now to abort."
sleep 5

# =============================================================
# PHASE 0 — DATA LOAD
# =============================================================
log_section "Phase 0: Data Load | ${CLUSTER_VERSION}"
log "Loading 2,000,000 documents × 20 fields × 1KB ≈ 40GB"
log "Expected duration: 45–60 minutes"
log "Start: $(date '+%Y-%m-%d %H:%M:%S')"

if [ "$TEST_MODE" = "true" ]; then
    log "TEST MODE: Skipping full data load — using existing collection data"
    log "In real mode, this loads 2,000,000 documents (~40GB)"
else
    $BINARY run --config "${CONFIGS}/zepto-load.yaml" \
        2>&1 | tee -a "$LOG_FILE" || log_error "Data load FAILED. Fix the error above and restart."
    log_success "Data load complete: $(date '+%Y-%m-%d %H:%M:%S')"
fi

# Verify load — uses patched config so warmup is short in TEST_MODE
log "Verifying load — checking highest key..."
VERIFY_CONFIG=$(make_verify_config "${CONFIGS}/zepto-run-workload-c.yaml")
$BINARY run \
    --config "$VERIFY_CONFIG" \
    --duration "${VERIFY_WAIT}s" \
    --threads 5 \
    --skip-preload \
    2>&1 | tee -a "$LOG_FILE" || log_warn "Verification run had issues — check output above"

echo ""
echo "  ┌─────────────────────────────────────────────────────┐"
echo "  │  CHECK THE OUTPUT ABOVE                             │"
echo "  │                                                     │"
echo "  │  Look for this line near the top:                   │"
echo "  │    Highest existing key : user1999999               │"
echo "  │                                                     │"
echo "  │  If the number is much less than 1999999,           │"
echo "  │  the load did not complete. Press Ctrl+C and        │"
echo "  │  rerun the load phase before continuing.            │"
echo "  └─────────────────────────────────────────────────────┘"
echo ""
read -rp "  Load verified? Press ENTER to start Phase 1, or Ctrl+C to abort: "

# =============================================================
# PHASE 1 — BENCHMARKS AT INITIAL TIER
# =============================================================
log_section "Phase 1: Benchmarks at ${INITIAL_TIER} | ${CLUSTER_VERSION}"
log "5 workloads × ${RUN_DURATION} run + ${WARMUP_DISPLAY} warmup + rest"
log "Phase 1 start: $(date '+%Y-%m-%d %H:%M:%S')"

run_phase "$INITIAL_TIER" "phase-1"

rest $REST_SECONDS "after workload-e phase-1 — waiting before scale-up"

log_success "Phase 1 complete: $(date '+%Y-%m-%d %H:%M:%S')"

# =============================================================
# SCALE-UP PAUSE
# =============================================================
log_section "Scale-Up: ${INITIAL_TIER} → ${SCALED_TIER} | ACTION REQUIRED"
echo ""
echo "  ┌─────────────────────────────────────────────────────┐"
echo "  │  ACTION REQUIRED ON BOTH ATLAS CLUSTERS NOW         │"
echo "  │                                                     │"
echo "  │  1. Open https://cloud.mongodb.com                  │"
echo "  │  2. Go to your project → Clusters                   │"
echo "  │  3. Scale v7 cluster: ${INITIAL_TIER} → ${SCALED_TIER}              │"
echo "  │  4. Scale v8 cluster: ${INITIAL_TIER} → ${SCALED_TIER}              │"
echo "  │  5. Atlas begins rolling restart (~10-20 min)       │"
echo "  │  6. Wait for BOTH clusters to show: Active          │"
echo "  │                                                     │"
echo "  │  Coordinate with the operator on EC2 Instance 1.   │"
echo "  │  Both should initiate scale-up at the same time.   │"
echo "  └─────────────────────────────────────────────────────┘"
echo ""
read -rp "  Have you INITIATED the scale-up on BOTH clusters? Press ENTER: "

log "Scale-up initiated. Waiting ${REST_SECONDS}s for Atlas rolling restart to complete..."
rest $REST_SECONDS "Atlas scale-up buffer — waiting for rolling restart"

# Pre-Phase 2 verification — uses patched config so warmup is short in TEST_MODE
log "Running pre-Phase 2 verification smoke test..."
VERIFY_CONFIG2=$(make_verify_config "${CONFIGS}/zepto-run-workload-c.yaml")
$BINARY run \
    --config "$VERIFY_CONFIG2" \
    --duration "${VERIFY_WAIT}s" \
    --threads 5 \
    --skip-preload \
    2>&1 | tee -a "$LOG_FILE" || log_warn "Verification smoke test had issues — check output above"

echo ""
echo "  ┌─────────────────────────────────────────────────────┐"
echo "  │  VERIFY BEFORE CONTINUING                           │"
echo "  │                                                     │"
echo "  │  1. Both Atlas clusters show: ${SCALED_TIER} tier           │"
echo "  │  2. Both Atlas clusters show: Active (green)        │"
echo "  │  3. Smoke test above shows: 0 errors                │"
echo "  │  4. Smoke test throughput similar to Phase 1        │"
echo "  └─────────────────────────────────────────────────────┘"
echo ""
read -rp "  Both clusters scaled and healthy? Press ENTER to start Phase 2: "

# =============================================================
# PHASE 2 — BENCHMARKS AT SCALED TIER
# =============================================================
log_section "Phase 2: Benchmarks at ${SCALED_TIER} | ${CLUSTER_VERSION}"
log "5 workloads × ${RUN_DURATION} run + ${WARMUP_DISPLAY} warmup + rest"
log "Phase 2 start: $(date '+%Y-%m-%d %H:%M:%S')"

run_phase "$SCALED_TIER" "phase-2"

log_success "Phase 2 complete: $(date '+%Y-%m-%d %H:%M:%S')"

# =============================================================
# DONE
# =============================================================
log_section "All Benchmarks Complete | ${CLUSTER_VERSION}"
echo ""
echo "  ┌─────────────────────────────────────────────────────┐"
echo "  │  BENCHMARK COMPLETE                                 │"
echo "  │                                                     │"
echo "  │  Cluster   : ${CLUSTER_VERSION}                              │"
echo "  │  Completed : $(date '+%Y-%m-%d %H:%M:%S')              │"
echo "  │  Log file  : ${LOG_FILE}       │"
echo "  │                                                     │"
echo "  │  Results   : ./results/zepto/                       │"
echo "  │  Reports   : ./reports/zepto/                       │"
echo "  │                                                     │"
echo "  │  NEXT STEPS:                                        │"
echo "  │  1. Confirm EC2 Instance 1 (v7) has also finished   │"
echo "  │  2. Download results from both instances            │"
echo "  │  3. Run comparison commands printed below           │"
echo "  └─────────────────────────────────────────────────────┘"
echo ""
log "Comparison commands:"
for workload in a b c d e; do
    wl_upper=$(echo "$workload" | tr '[:lower:]' '[:upper:]')
    log "  # Workload ${wl_upper} — ${INITIAL_TIER}:"
    log "  go run main.go compare --config ${CONFIGS}/zepto-run-workload-${workload}.yaml --tag-a \"v7,workload-${workload},phase-1,${INITIAL_TIER}\" --tag-b \"v8,workload-${workload},phase-1,${INITIAL_TIER}\" --output both"
    log "  # Workload ${wl_upper} — ${SCALED_TIER}:"
    log "  go run main.go compare --config ${CONFIGS}/zepto-run-workload-${workload}.yaml --tag-a \"v7,workload-${workload},phase-2,${SCALED_TIER}\" --tag-b \"v8,workload-${workload},phase-2,${SCALED_TIER}\" --output both"
done