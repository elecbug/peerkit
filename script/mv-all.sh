#!/usr/bin/env bash

set -euo pipefail

# This script must be executed from the project root directory.
# It collects experiment result directories from:
#
#   .peerkit/runs/<DATE>/<EXPERIMENT>/
#
# and moves them into:
#
#   nas/results/<EXP_NAME>/
#
# Usage:
#   ./script/mv-all.sh <EXP_NAME>

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <EXP_NAME>" >&2
    exit 1
fi

EXP_NAME="$1"

# Use the current working directory as the project root.
PROJECT_ROOT="$(pwd)"

RUNS_DIR="$PROJECT_ROOT/.peerkit/runs"
DEST_DIR="$PROJECT_ROOT/nas/results/$EXP_NAME"

# Verify that the script is being executed from the project root.
if [[ ! -d "$PROJECT_ROOT/script" ]] ||
   [[ ! -d "$PROJECT_ROOT/.peerkit" ]] ||
   [[ ! -d "$PROJECT_ROOT/nas" ]]; then
    echo "Error: This script must be executed from the project root directory." >&2
    echo "Current directory: $PROJECT_ROOT" >&2
    exit 1
fi

# Verify that the PeerKit runs directory exists.
if [[ ! -d "$RUNS_DIR" ]]; then
    echo "Error: PeerKit runs directory does not exist." >&2
    echo "Path: $RUNS_DIR" >&2
    exit 1
fi

# Collect directories located exactly two levels below the runs directory.
#
# Example:
#   .peerkit/runs/260731/experiment-a
#   .peerkit/runs/260801/experiment-b
mapfile -d '' -t SOURCE_DIRS < <(
    find "$RUNS_DIR" \
        -mindepth 2 \
        -maxdepth 2 \
        -type d \
        -print0 |
        sort -z
)

if (( ${#SOURCE_DIRS[@]} == 0 )); then
    echo "[INFO] No experiment result directories were found."
    exit 0
fi

# Check for duplicate experiment directory names before moving anything.
#
# This prevents results from different date directories from accidentally
# overwriting or merging with one another.
declare -A SEEN_NAMES=()

for SOURCE_DIR in "${SOURCE_DIRS[@]}"; do
    DIR_NAME="$(basename "$SOURCE_DIR")"

    if [[ -n "${SEEN_NAMES[$DIR_NAME]+x}" ]]; then
        echo "Error: Duplicate experiment directory name detected." >&2
        echo "Name: $DIR_NAME" >&2
        echo "First path:  ${SEEN_NAMES[$DIR_NAME]}" >&2
        echo "Second path: $SOURCE_DIR" >&2
        echo "No directories were moved." >&2
        exit 1
    fi

    SEEN_NAMES["$DIR_NAME"]="$SOURCE_DIR"
done

# Check whether any destination directory already exists.
#
# The script aborts before moving anything to avoid partial collection.
for SOURCE_DIR in "${SOURCE_DIRS[@]}"; do
    DIR_NAME="$(basename "$SOURCE_DIR")"
    TARGET_DIR="$DEST_DIR/$DIR_NAME"

    if [[ -e "$TARGET_DIR" ]]; then
        echo "Error: Destination already contains an entry with the same name." >&2
        echo "Source:      $SOURCE_DIR" >&2
        echo "Destination: $TARGET_DIR" >&2
        echo "No directories were moved." >&2
        exit 1
    fi
done

# Create the destination directory.
mkdir -p "$DEST_DIR"

echo "[INFO] Collecting experiment result directories."
echo "Source root: $RUNS_DIR"
echo "Destination: $DEST_DIR"
echo "Directory count: ${#SOURCE_DIRS[@]}"

# Move each experiment result directory into the destination directory.
for SOURCE_DIR in "${SOURCE_DIRS[@]}"; do
    DIR_NAME="$(basename "$SOURCE_DIR")"

    echo "[MOVE] $SOURCE_DIR"
    echo "       -> $DEST_DIR/$DIR_NAME"

    mv -- "$SOURCE_DIR" "$DEST_DIR/$DIR_NAME"
done

# Remove empty date directories after all experiment directories are moved.
find "$RUNS_DIR" \
    -mindepth 1 \
    -maxdepth 1 \
    -type d \
    -empty \
    -delete

echo "[DONE] All experiment result directories were moved successfully."