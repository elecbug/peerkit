#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 '<file-pattern>'" >&2
    exit 1
fi

pattern="$1"

shopt -s nullglob

files=( $pattern )

if [[ ${#files[@]} -eq 0 ]]; then
    echo "No files matched: $pattern" >&2
    exit 1
fi

for file in "${files[@]}"; do
    [[ -f "$file" ]] || continue

    echo "===== $file ====="
    cat "$file"
    echo
done