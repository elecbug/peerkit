#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SCRIPT_DIR/extract_metrics.py"
OUTPUT_DIR="$SCRIPT_DIR/extracted-metrics"

N_VALUES=(100 200 300 400 500)
K_VALUES=(4 8 16 32 64)

BETA=0.2
RUNS=100
WORKERS=0
SEED=42

mkdir -p "$OUTPUT_DIR"

for N in "${N_VALUES[@]}"; do
    for K in "${K_VALUES[@]}"; do
        M=$((K / 2))

        NAME="n${N}-k${K}"

        echo "========================================"
        echo "Running ${NAME}"
        echo "N=${N}, k=${K}, m=${M}, beta=${BETA}"
        echo "========================================"

        python3 "$SCRIPT" \
            -N "$N" \
            -k "$K" \
            -m "$M" \
            -b "$BETA" \
            -r "$RUNS" \
            -w "$WORKERS" \
            --seed "$SEED" \
            --csv "${OUTPUT_DIR}/${NAME}.csv" \
            | tee "${OUTPUT_DIR}/${NAME}.txt"

        echo
    done
done

echo "All topology metric runs completed."
echo "Results: ${OUTPUT_DIR}"
