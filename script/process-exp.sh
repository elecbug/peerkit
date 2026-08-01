#!/usr/bin/env bash

set -euo pipefail

# This script must be executed from the project root directory.
# Usage:
#   ./script/process-exp.sh <CATEGORY_DIR> <EXP_NAME>

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <CATEGORY_DIR> <EXP_NAME>" >&2
    exit 1
fi

CATEGORY_DIR="$1"
EXP_NAME="$2"

# Use the current working directory as the project root.
PROJECT_ROOT="$(pwd)"

VENV_DIR="$PROJECT_ROOT/venv"

RESULT_DIR="$PROJECT_ROOT/nas/$CATEGORY_DIR/result/$EXP_NAME"
EXP_DIR="$PROJECT_ROOT/exp/$EXP_NAME"
OUT_DIR="$EXP_DIR/out"
SUMMARY_DIR="$OUT_DIR/summary"
OUT_CSV="$OUT_DIR/out.csv"

NAS_EXP_ROOT="$PROJECT_ROOT/nas/$CATEGORY_DIR/exp"
NAS_EXP_DIR="$NAS_EXP_ROOT/$EXP_NAME"

SUMMARY_SCRIPT="$PROJECT_ROOT/script/summary.py"
PLOT_SCRIPT="$PROJECT_ROOT/script/plot_summary.py"

# Verify that the script is being executed from the project root.
if [[ ! -d "$PROJECT_ROOT/script" ]] ||
   [[ ! -d "$PROJECT_ROOT/exp" ]] ||
   [[ ! -d "$PROJECT_ROOT/nas" ]]; then
    echo "Error: This script must be executed from the project root directory." >&2
    echo "Current directory: $PROJECT_ROOT" >&2
    exit 1
fi

# Verify that Python 3 is available.
if ! command -v python3 >/dev/null 2>&1; then
    echo "Error: python3 is not installed or is not available in PATH." >&2
    exit 1
fi

# Verify that the experiment result directory exists.
if [[ ! -d "$RESULT_DIR" ]]; then
    echo "Error: Experiment result directory does not exist." >&2
    echo "Path: $RESULT_DIR" >&2
    exit 1
fi

# Verify that the experiment directory exists.
if [[ ! -d "$EXP_DIR" ]]; then
    echo "Error: Experiment directory does not exist." >&2
    echo "Path: $EXP_DIR" >&2
    exit 1
fi

# Verify that the required Python scripts exist.
if [[ ! -f "$SUMMARY_SCRIPT" ]]; then
    echo "Error: summary.py was not found." >&2
    echo "Path: $SUMMARY_SCRIPT" >&2
    exit 1
fi

if [[ ! -f "$PLOT_SCRIPT" ]]; then
    echo "Error: plot_summary.py was not found." >&2
    echo "Path: $PLOT_SCRIPT" >&2
    exit 1
fi

# Create output directories if they do not exist.
mkdir -p "$OUT_DIR"
mkdir -p "$SUMMARY_DIR"

# Create the virtual environment if it does not exist.
if [[ ! -f "$VENV_DIR/bin/activate" ]]; then
    echo "[INFO] Creating Python virtual environment: $VENV_DIR"

    if ! python3 -m venv "$VENV_DIR"; then
        echo "Error: Failed to create the Python virtual environment." >&2
        echo "Install the python3-venv package and try again." >&2
        exit 1
    fi
fi

EXPECTED_VENV="$(cd "$VENV_DIR" && pwd)"

# Activate the project virtual environment if it is not currently active.
if [[ "${VIRTUAL_ENV:-}" != "$EXPECTED_VENV" ]]; then
    echo "[INFO] Activating Python virtual environment: $VENV_DIR"

    # shellcheck disable=SC1091
    source "$VENV_DIR/bin/activate"
else
    echo "[INFO] The project virtual environment is already active."
fi

# Verify that the activated Python interpreter belongs to the project venv.
ACTIVE_PYTHON="$(command -v python3)"

if [[ "$ACTIVE_PYTHON" != "$VENV_DIR/bin/python3" ]]; then
    echo "Error: Failed to activate the project virtual environment." >&2
    echo "Active Python interpreter: $ACTIVE_PYTHON" >&2
    exit 1
fi

# Detect missing Python packages.
MISSING_PACKAGES=()

if ! python3 -c "import pandas" >/dev/null 2>&1; then
    MISSING_PACKAGES+=("pandas")
fi

if ! python3 -c "import matplotlib" >/dev/null 2>&1; then
    MISSING_PACKAGES+=("matplotlib")
fi

# Install only the missing Python packages.
if (( ${#MISSING_PACKAGES[@]} > 0 )); then
    echo "[INFO] Installing missing Python packages: ${MISSING_PACKAGES[*]}"

    python3 -m pip install --upgrade pip
    python3 -m pip install "${MISSING_PACKAGES[@]}"
else
    echo "[INFO] All required Python packages are already installed."
fi

# Generate the experiment summary CSV file.
echo "[INFO] Generating experiment summary: $EXP_NAME"

python3 "$SUMMARY_SCRIPT" \
    --dir "$RESULT_DIR" \
    --out "$OUT_CSV"

# Verify that the summary CSV file was generated.
if [[ ! -f "$OUT_CSV" ]]; then
    echo "Error: The summary CSV file was not generated." >&2
    echo "Expected path: $OUT_CSV" >&2
    exit 1
fi

# Generate summary plots.
echo "[INFO] Generating summary plots."

python3 "$PLOT_SCRIPT" \
    --in "$OUT_CSV" \
    --out-dir "$SUMMARY_DIR"

# Copy the complete experiment directory to the NAS experiment directory.
echo "[INFO] Copying the experiment directory to NAS."
echo "Source:      $EXP_DIR"
echo "Destination: $NAS_EXP_DIR"

sudo mkdir -p "$NAS_EXP_DIR"
sudo cp -a "$EXP_DIR/." "$NAS_EXP_DIR/"

echo "[DONE] Experiment processing completed successfully: $EXP_NAME"