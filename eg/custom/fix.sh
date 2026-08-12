#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <yaml-directory>" >&2
    exit 1
fi

TARGET_DIR="$1"

if [[ ! -d "$TARGET_DIR" ]]; then
    echo "Directory not found: $TARGET_DIR" >&2
    exit 1
fi

if ! command -v yq >/dev/null 2>&1; then
    echo "yq v4 is required." >&2
    exit 1
fi

UPDATED=0

while IFS= read -r -d '' FILE; do
    echo "Updating: $FILE"

    yq -y -i '
        .domain.topology.model = "CHANGE"
    ' "$FILE"

    UPDATED=$((UPDATED + 1))
done < <(
    find "$TARGET_DIR" \
        -type f \
        \( -name '*.yaml' -o -name '*.yml' \) \
        -print0
)

echo "Updated $UPDATED YAML file(s)."