#!/usr/bin/env bash
set -uo pipefail

IMAGE="localhost:5000/peerkit-peer:dev"

EXP_DIR="${1:?Usage: $0 <experiment-dir> <repeat> [keyword-file]}"
REPEAT="${2:?Usage: $0 <experiment-dir> <repeat> [keyword-file]}"
KEYWORD_FILE="${3:-KEYWORD}"

LOG_ROOT="batch-logs/$(date '+%Y%m%d-%H%M%S')"

mkdir -p "$LOG_ROOT"

if [[ ! -f "$KEYWORD_FILE" ]]; then
    echo "Keyword file not found: $KEYWORD_FILE" >&2
    exit 1
fi

declare -a KEYWORDS=()

while IFS= read -r LINE || [[ -n "$LINE" ]]; do
    LINE="${LINE%$'\r'}"

    LINE="${LINE#"${LINE%%[![:space:]]*}"}"
    LINE="${LINE%"${LINE##*[![:space:]]}"}"

    [[ -z "$LINE" || "$LINE" == \#* ]] && continue

    KEYWORDS+=("$LINE")
done < "$KEYWORD_FILE"

if (( ${#KEYWORDS[@]} == 0 )); then
    echo "No valid keywords found in: $KEYWORD_FILE" >&2
    exit 1
fi

matches_keyword() {
    local NAME="$1"
    local KEYWORD

    for KEYWORD in "${KEYWORDS[@]}"; do
        if [[ "$NAME" == *"$KEYWORD"* ]]; then
            return 0
        fi
    done

    return 1
}

cleanup_matching_docker_resources() {
    local -a SERVICE_IDS=()
    local -a NETWORK_IDS=()

    local ID
    local NAME
    local ATTEMPT

    echo
    echo "=== Cleaning matching Docker resources ==="

    while IFS=$'\t' read -r ID NAME; do
        [[ -z "$ID" || -z "$NAME" ]] && continue

        if matches_keyword "$NAME"; then
            echo "[service] removing: $NAME ($ID)"
            SERVICE_IDS+=("$ID")
        fi
    done < <(
        sudo docker service ls \
            --format $'{{.ID}}\t{{.Name}}' \
            2>/dev/null
    )

    if (( ${#SERVICE_IDS[@]} > 0 )); then
        if ! sudo docker service rm "${SERVICE_IDS[@]}"; then
            echo "Warning: some services could not be removed." >&2
        fi
    else
        echo "[service] no matching services"
    fi

    if (( ${#SERVICE_IDS[@]} > 0 )); then
        for ATTEMPT in {1..30}; do
            local REMAINING=0

            for ID in "${SERVICE_IDS[@]}"; do
                if sudo docker service inspect "$ID" \
                    >/dev/null 2>&1
                then
                    REMAINING=1
                    break
                fi
            done

            if (( REMAINING == 0 )); then
                break
            fi

            sleep 1
        done
    fi

    while IFS=$'\t' read -r ID NAME; do
        [[ -z "$ID" || -z "$NAME" ]] && continue

        if ! matches_keyword "$NAME"; then
            continue
        fi

        case "$NAME" in
            bridge|host|none|ingress|docker_gwbridge)
                echo "[network] protected, skipping: $NAME"
                continue
                ;;
        esac

        echo "[network] selected: $NAME ($ID)"
        NETWORK_IDS+=("$ID")
    done < <(
        sudo docker network ls \
            --format $'{{.ID}}\t{{.Name}}'
    )

    if (( ${#NETWORK_IDS[@]} == 0 )); then
        echo "[network] no matching networks"
    else
        for ID in "${NETWORK_IDS[@]}"; do
            for ATTEMPT in {1..30}; do
                if ! sudo docker network inspect "$ID" \
                    >/dev/null 2>&1
                then
                    echo "[network] already removed: $ID"
                    break
                fi

                if sudo docker network rm "$ID" \
                    >/dev/null 2>&1
                then
                    echo "[network] removed: $ID"
                    break
                fi

                if (( ATTEMPT == 30 )); then
                    echo "[network] failed to remove: $ID" >&2

                    sudo docker network inspect "$ID" \
                        --format \
                        'name={{.Name}} driver={{.Driver}} scope={{.Scope}} containers={{len .Containers}}' \
                        2>/dev/null || true
                else
                    sleep 1
                fi
            done
        done
    fi

    echo "=== Docker resource cleanup completed ==="
}

echo "=== Keywords ==="
printf '  - %s\n' "${KEYWORDS[@]}"

echo
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
        echo "Preparing: $(date '+%Y-%m-%d %H:%M:%S')"
        echo "============================================================"

        cleanup_matching_docker_resources \
            2>&1 | tee -a "$LOG_FILE"

        echo
        echo "Started: $(date '+%Y-%m-%d %H:%M:%S')" |
            tee -a "$LOG_FILE"

        if sudo ./bin/peerkit run \
            --image "$IMAGE" \
            "$SCENARIO" \
            2>&1 | tee -a "$LOG_FILE"
        then
            echo "SUCCESS: $SCENARIO run $RUN_INDEX" |
                tee -a "$LOG_FILE"
        else
            STATUS=${PIPESTATUS[0]}
            FAILURES=$((FAILURES + 1))

            echo "FAILED: $SCENARIO run $RUN_INDEX, exit=$STATUS" |
                tee -a "$LOG_FILE"

            sudo ./bin/peerkit stop \
                "$SCENARIO" \
                2>&1 | tee -a "$LOG_FILE"
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