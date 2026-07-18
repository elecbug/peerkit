#!/usr/bin/env bash
set -uo pipefail

IMAGE="localhost:5000/peerkit-peer:dev"
EXP_DIR=$1
REPEAT=10
LOG_ROOT="batch-logs/$(date '+%Y%m%d-%H%M%S')"

mkdir -p "$LOG_ROOT"

echo "=== Building peerkit ==="
make build || {
    echo "Build failed."
    exit 1
}

sudo -v || exit 1

mapfile -d '' SCENARIOS < <(
    find "$EXP_DIR" \
        -maxdepth 1 \
        -type f \
        \( -name '*.yaml' -o -name '*.yml' \) \
        -print0 |
    sort -z
)

if (( ${#SCENARIOS[@]} == 0 )); then
    echo "No YAML files found in $EXP_DIR"
    exit 1
fi

TOTAL=$((${#SCENARIOS[@]} * REPEAT))
CURRENT=0
FAILURES=0

for RUN_INDEX in $(seq 1 "$REPEAT"); do
    for SCENARIO in "${SCENARIOS[@]}"; do
        CURRENT=$((CURRENT + 1))

        NAME=$(basename "$SCENARIO")
        NAME="${NAME%.*}"

        mkdir -p "$LOG_ROOT/$NAME"

        LOG_FILE=$(printf \
            '%s/%s/run-%02d.log' \
            "$LOG_ROOT" \
            "$NAME" \
            "$RUN_INDEX"
        )

        echo
        echo "============================================================"
        echo "[$CURRENT/$TOTAL] Scenario: $SCENARIO"
        echo "Run: $RUN_INDEX/$REPEAT"
        echo "Log: $LOG_FILE"
        echo "Started: $(date '+%Y-%m-%d %H:%M:%S')"
        echo "============================================================"

        if sudo ./bin/peerkit run \
            --image "$IMAGE" \
            "$SCENARIO" \
            2>&1 | tee "$LOG_FILE"
        then
            echo "SUCCESS: $SCENARIO run $RUN_INDEX"
        else
            STATUS=${PIPESTATUS[0]}
            FAILURES=$((FAILURES + 1))

            echo "FAILED: $SCENARIO run $RUN_INDEX, exit=$STATUS" |
                tee -a "$LOG_FILE"

            sudo ./bin/peerkit stop \
            "$SCENARIO" \
            2>&1 | tee "$LOG_FILE"
        fi
    done
done

echo
echo "============================================================"
echo "Batch completed"
echo "Total runs: $TOTAL"
echo "Successful: $((TOTAL - FAILURES))"
echo "Failed: $FAILURES"
echo "Logs: $LOG_ROOT"
echo "============================================================"

if (( FAILURES > 0 )); then
    exit 1
fi