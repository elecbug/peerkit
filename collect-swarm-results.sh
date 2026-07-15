#!/usr/bin/env bash
set -Eeuo pipefail

PROGRAM="$(basename "$0")"
TIMEOUT_SECONDS=1800
POLL_SECONDS=2
REMOVE_TIMEOUT_SECONDS=180
KEEP_STACK=false
REMOVE_ON_FAILURE=false

usage() {
  cat <<USAGE
Usage:
  $PROGRAM [options] RUN_DIR

Wait for a peerkit Swarm experiment, download and extract its result archive,
and remove the Swarm stack only after the results have been saved successfully.

Arguments:
  RUN_DIR                    peerkit run directory containing run.yaml

Options:
  --timeout SECONDS          Maximum wait for experiment completion (default: 1800)
  --poll SECONDS             Status polling interval (default: 2)
  --remove-timeout SECONDS   Maximum wait for stack removal (default: 180)
  --keep-stack               Keep the Swarm stack after successful collection
  --remove-on-failure        Remove the stack even if the experiment reports failure
  -h, --help                 Show this help

Example:
  $PROGRAM \
    /home/elecbug/src/peerkit/.peerkit/runs/peerkit-er-base-flooding-1784122218647915040
USAGE
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

read_root_yaml_scalar() {
  local key="$1"
  local file="$2"

  awk -v wanted="$key" '
    /^[[:space:]]*#/ { next }
    $0 ~ "^" wanted ":[[:space:]]*" {
      value = $0
      sub("^" wanted ":[[:space:]]*", "", value)
      sub(/[[:space:]]+#.*$/, "", value)
      if ((substr(value, 1, 1) == "\"") && (substr(value, length(value), 1) == "\"")) {
        value = substr(value, 2, length(value) - 2)
      } else if ((substr(value, 1, 1) == "\047") && (substr(value, length(value), 1) == "\047")) {
        value = substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$file"
}

stack_exists() {
  docker stack ls --format '{{.Name}}' 2>/dev/null | grep -Fxq -- "$PROJECT_NAME"
}

remove_stack() {
  if ! stack_exists; then
    printf 'Swarm stack is already absent: %s\n' "$PROJECT_NAME"
    return 0
  fi

  printf 'Removing Swarm stack: %s\n' "$PROJECT_NAME"
  docker stack rm "$PROJECT_NAME"

  local deadline=$((SECONDS + REMOVE_TIMEOUT_SECONDS))
  while stack_exists; do
    if (( SECONDS >= deadline )); then
      printf 'warning: stack removal is still in progress after %ss: %s\n' \
        "$REMOVE_TIMEOUT_SECONDS" "$PROJECT_NAME" >&2
      return 1
    fi
    sleep 1
  done

  printf 'Swarm stack removed: %s\n' "$PROJECT_NAME"
}

save_diagnostics() {
  local destination="$RUN_DIR/diagnostics"
  mkdir -p "$destination"

  if [[ -n "${LAST_STATUS_FILE:-}" && -f "$LAST_STATUS_FILE" ]]; then
    cp "$LAST_STATUS_FILE" "$destination/controller-status.json" || true
  fi

  docker stack services "$PROJECT_NAME" --no-trunc \
    >"$destination/stack-services.txt" 2>&1 || true
  docker stack ps "$PROJECT_NAME" --no-trunc \
    >"$destination/stack-tasks.txt" 2>&1 || true
  docker service logs --timestamps --no-trunc "${PROJECT_NAME}_controller" \
    >"$destination/controller.log" 2>&1 || true

  printf 'Diagnostics saved to: %s\n' "$destination" >&2
}

while (($# > 0)); do
  case "$1" in
    --timeout)
      (($# >= 2)) || fail "--timeout requires a value"
      TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    --poll)
      (($# >= 2)) || fail "--poll requires a value"
      POLL_SECONDS="$2"
      shift 2
      ;;
    --remove-timeout)
      (($# >= 2)) || fail "--remove-timeout requires a value"
      REMOVE_TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    --keep-stack)
      KEEP_STACK=true
      shift
      ;;
    --remove-on-failure)
      REMOVE_ON_FAILURE=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      fail "unknown option: $1"
      ;;
    *)
      break
      ;;
  esac
done

(($# == 1)) || {
  usage >&2
  exit 2
}

RUN_DIR="${1%/}"
RUN_METADATA="$RUN_DIR/run.yaml"

[[ -d "$RUN_DIR" ]] || fail "run directory not found: $RUN_DIR"
[[ -f "$RUN_METADATA" ]] || fail "run metadata not found: $RUN_METADATA"
[[ "$TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] && (( TIMEOUT_SECONDS > 0 )) \
  || fail "--timeout must be a positive integer"
[[ "$POLL_SECONDS" =~ ^[0-9]+([.][0-9]+)?$ ]] \
  || fail "--poll must be a positive number"
[[ "$REMOVE_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] && (( REMOVE_TIMEOUT_SECONDS > 0 )) \
  || fail "--remove-timeout must be a positive integer"

require_command curl
require_command docker
require_command python3
require_command tar

DEPLOYMENT_MODE="$(read_root_yaml_scalar deployment_mode "$RUN_METADATA")"
PROJECT_NAME="$(read_root_yaml_scalar project_name "$RUN_METADATA")"
CONTROLLER_URL="$(read_root_yaml_scalar controller_url "$RUN_METADATA")"

[[ "$DEPLOYMENT_MODE" == "swarm" ]] \
  || fail "run is not a Swarm deployment: deployment_mode=${DEPLOYMENT_MODE:-missing}"
[[ -n "$PROJECT_NAME" ]] || fail "project_name is missing from $RUN_METADATA"
[[ -n "$CONTROLLER_URL" ]] || fail "controller_url is missing from $RUN_METADATA"

CONTROLLER_URL="${CONTROLLER_URL%/}"
RESULT_DIR="$RUN_DIR/results"
ARCHIVE_FINAL="$RUN_DIR/peerkit-results.tar.gz"
ARCHIVE_TEMP="$RUN_DIR/.peerkit-results.tar.gz.part.$$"
EXTRACT_TEMP="$RUN_DIR/.results.extract.$$"
LAST_STATUS_FILE="$RUN_DIR/.controller-status.$$.json"

cleanup_temp() {
  rm -f "$ARCHIVE_TEMP" "$LAST_STATUS_FILE"
  rm -rf "$EXTRACT_TEMP"
}
trap cleanup_temp EXIT

printf 'Run directory : %s\n' "$RUN_DIR"
printf 'Swarm stack  : %s\n' "$PROJECT_NAME"
printf 'Controller   : %s\n' "$CONTROLLER_URL"
printf 'Waiting for experiment completion...\n'

start_epoch="$(date +%s)"
last_line=""
state=""
error_message=""

while true; do
  http_code="$(
    curl --silent --show-error \
      --connect-timeout 5 \
      --max-time 20 \
      --output "$LAST_STATUS_FILE" \
      --write-out '%{http_code}' \
      "$CONTROLLER_URL/v1/status" 2>/dev/null || printf '000'
  )"

  if [[ "$http_code" == "200" ]]; then
    IFS=$'\t' read -r state registered expected error_message < <(
      python3 - "$LAST_STATUS_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as stream:
    data = json.load(stream)

def clean(value):
    return str(value if value is not None else "").replace("\t", " ").replace("\n", " ")

print("\t".join([
    clean(data.get("state", "")),
    clean(data.get("registered", 0)),
    clean(data.get("expected", 0)),
    clean(data.get("error", "")),
]))
PY
    )

    line="state=${state:-unknown} peers=${registered:-0}/${expected:-0}"
    if [[ "$line" != "$last_line" ]]; then
      printf '%s\n' "$line"
      last_line="$line"
    fi

    case "$state" in
      completed)
        break
        ;;
      failed)
        printf 'Experiment failed: %s\n' "${error_message:-unknown controller error}" >&2
        save_diagnostics
        if [[ "$REMOVE_ON_FAILURE" == true ]]; then
          remove_stack || true
        else
          printf 'The stack was kept for inspection. Use --remove-on-failure to remove it automatically.\n' >&2
        fi
        exit 1
        ;;
    esac
  elif [[ "$http_code" != "000" ]]; then
    printf 'Controller status returned HTTP %s; retrying...\n' "$http_code" >&2
  fi

  now_epoch="$(date +%s)"
  if (( now_epoch - start_epoch >= TIMEOUT_SECONDS )); then
    printf 'Timed out after %ss while waiting for the experiment.\n' "$TIMEOUT_SECONDS" >&2
    save_diagnostics
    if [[ "$REMOVE_ON_FAILURE" == true ]]; then
      remove_stack || true
    else
      printf 'The stack was kept for inspection.\n' >&2
    fi
    exit 1
  fi

  sleep "$POLL_SECONDS"
done

printf 'Experiment completed. Downloading result archive...\n'
curl --fail --location --show-error \
  --retry 3 \
  --retry-delay 2 \
  --connect-timeout 10 \
  --max-time 600 \
  "$CONTROLLER_URL/v1/results/archive" \
  --output "$ARCHIVE_TEMP"

printf 'Validating result archive...\n'
tar -tzf "$ARCHIVE_TEMP" >/dev/null

mkdir -p "$EXTRACT_TEMP"
tar -xzf "$ARCHIVE_TEMP" -C "$EXTRACT_TEMP"

[[ -f "$EXTRACT_TEMP/summary.json" ]] \
  || fail "downloaded archive does not contain summary.json; stack was not removed"
[[ -f "$EXTRACT_TEMP/messages.csv" ]] \
  || fail "downloaded archive does not contain messages.csv; stack was not removed"

rm -rf "$RESULT_DIR"
mv "$EXTRACT_TEMP" "$RESULT_DIR"
mv "$ARCHIVE_TEMP" "$ARCHIVE_FINAL"

printf 'Results saved : %s\n' "$RESULT_DIR"
printf 'Archive saved : %s\n' "$ARCHIVE_FINAL"

if [[ "$KEEP_STACK" == true ]]; then
  printf 'Keeping Swarm stack as requested: %s\n' "$PROJECT_NAME"
else
  remove_stack
fi

printf 'Done.\n'