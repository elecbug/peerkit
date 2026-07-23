#!/usr/bin/env python3
"""
Aggregate repeated topology experiments without pseudo-replication.

Aggregation procedure
---------------------
1. Each messages.csv is treated as one topology-instance run.
2. For every metric, the arithmetic mean over all messages in that run
   is calculated.
3. Runs with the same normalized experiment name are grouped.
4. Across the run-level means, the script reports:
   mean, median, Q1, Q3, minimum, and maximum.

Therefore, an output field such as completion_delay_ms_median means:
    median of the topology-instance-level mean completion delays.

Q1 and Q3 use Python's inclusive quartile definition.
"""

from __future__ import annotations

import argparse
import csv
import math
import re
import statistics
import sys
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Iterable


TARGET_COLUMNS = (
    "reached_nodes",
    "total_nodes",
    "reachability",
    "completion_delay_ms",
    "transmissions",
    "duplicates",
    "drops",
    "suppressions",
    "control_sent",
    "control_received",
    "control_drops",
    "control_bytes_sent",
)

GROUP_STATISTICS = ("mean", "median", "q1", "q3", "min", "max")

# Example:
#   260718/ba-base-flooding-t105948/results/messages.csv
#                         ^^^^^^^^^^^^^^^^^^^^^^^
# becomes:
#   ba-base-flooding
RUN_DIRECTORY_PATTERN = re.compile(r"^(?P<name>.+)-t[^/]+$")


@dataclass(frozen=True)
class RunSummary:
    experiment: str
    relative_file: str
    message_count: int
    metric_means: dict[str, float]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Summarize each messages.csv as one topology-instance run, "
            "then aggregate the run-level means by experiment."
        )
    )
    parser.add_argument(
        "--dir",
        required=True,
        type=Path,
        dest="root_dir",
        help="Root directory searched recursively for messages.csv files.",
    )
    parser.add_argument(
        "--out",
        required=True,
        type=Path,
        dest="output_file",
        help="Output CSV containing statistics across topology-instance means.",
    )
    parser.add_argument(
        "--runs-out",
        type=Path,
        default=None,
        help=(
            "Optional CSV containing one row per topology-instance run "
            "and its message-level metric means."
        ),
    )
    parser.add_argument(
        "--expected-messages",
        type=int,
        default=100,
        help=(
            "Expected number of message rows per run. A mismatch produces "
            "a warning but does not discard the run. Set to 0 to disable."
        ),
    )
    return parser.parse_args()


def parse_number(value: object) -> float | None:
    text = str(value).strip()
    if not text:
        return None

    try:
        number = float(text)
    except (TypeError, ValueError):
        return None

    if not math.isfinite(number):
        return None

    return number


def normalize_number(value: float | int) -> int | float:
    number = float(value)
    if number.is_integer():
        return int(number)
    return number


def experiment_key(relative_file: str) -> str:
    """Extract the repeated-experiment name from a path component ending in -t..."""
    parts = PurePosixPath(relative_file).parts

    for part in parts:
        match = RUN_DIRECTORY_PATTERN.fullmatch(part)
        if match:
            return match.group("name")

    if len(parts) >= 3 and parts[-2] == "results":
        fallback = parts[-3]
    elif len(parts) >= 2:
        fallback = parts[-2]
    else:
        fallback = PurePosixPath(relative_file).stem

    print(
        f"[warning] no '-t...' run suffix found in {relative_file!r}; "
        f"using {fallback!r} as the experiment key",
        file=sys.stderr,
    )
    return fallback


def summarize_run(csv_path: Path, root_dir: Path) -> RunSummary:
    values: dict[str, list[float]] = {
        column: [] for column in TARGET_COLUMNS
    }
    message_count = 0

    with csv_path.open("r", encoding="utf-8-sig", newline="") as file:
        reader = csv.DictReader(file)

        if reader.fieldnames is None:
            raise ValueError("CSV header is missing")

        missing_columns = [
            column for column in TARGET_COLUMNS
            if column not in reader.fieldnames
        ]
        if missing_columns:
            raise ValueError(
                "required columns are missing: " + ", ".join(missing_columns)
            )

        for row_number, row in enumerate(reader, start=2):
            message_count += 1

            for column in TARGET_COLUMNS:
                raw_value = row.get(column, "")
                number = parse_number(raw_value)

                if number is None:
                    print(
                        f"[warning] {csv_path}:{row_number}: "
                        f"invalid value for {column!r}: {raw_value!r}",
                        file=sys.stderr,
                    )
                    continue

                values[column].append(number)

    if message_count == 0:
        raise ValueError("messages.csv contains no data rows")

    metric_means: dict[str, float] = {}
    for column, column_values in values.items():
        if not column_values:
            raise ValueError(f"no valid values found for {column!r}")

        if len(column_values) != message_count:
            print(
                f"[warning] {csv_path}: {column!r} has "
                f"{len(column_values)} valid values for {message_count} rows",
                file=sys.stderr,
            )

        metric_means[column] = statistics.fmean(column_values)

    relative_file = csv_path.relative_to(root_dir).as_posix()

    return RunSummary(
        experiment=experiment_key(relative_file),
        relative_file=relative_file,
        message_count=message_count,
        metric_means=metric_means,
    )


def quartiles_inclusive(values: list[float]) -> tuple[float, float]:
    if not values:
        raise ValueError("cannot calculate quartiles of an empty list")

    if len(values) == 1:
        return values[0], values[0]

    q1, _, q3 = statistics.quantiles(values, n=4, method="inclusive")
    return q1, q3


def group_statistics(values: list[float]) -> dict[str, float]:
    if not values:
        raise ValueError("cannot summarize an empty list")

    q1, q3 = quartiles_inclusive(values)

    return {
        "mean": statistics.fmean(values),
        "median": statistics.median(values),
        "q1": q1,
        "q3": q3,
        "min": min(values),
        "max": max(values),
    }


def aggregate_runs(runs: Iterable[RunSummary]) -> list[dict[str, object]]:
    groups: dict[str, list[RunSummary]] = defaultdict(list)

    for run in runs:
        groups[run.experiment].append(run)

    rows: list[dict[str, object]] = []

    for experiment in sorted(groups):
        group = groups[experiment]
        message_counts = [run.message_count for run in group]

        row: dict[str, object] = {
            "file": experiment,
            "runs": len(group),
            "messages_per_run_mean": normalize_number(
                statistics.fmean(message_counts)
            ),
            "messages_per_run_min": min(message_counts),
            "messages_per_run_max": max(message_counts),
        }

        for metric in TARGET_COLUMNS:
            topology_means = [run.metric_means[metric] for run in group]
            stats = group_statistics(topology_means)

            for statistic_name in GROUP_STATISTICS:
                row[f"{metric}_{statistic_name}"] = normalize_number(
                    stats[statistic_name]
                )

        rows.append(row)

    return rows


def aggregate_fieldnames() -> list[str]:
    fields = [
        "file",
        "runs",
        "messages_per_run_mean",
        "messages_per_run_min",
        "messages_per_run_max",
    ]

    for metric in TARGET_COLUMNS:
        for statistic_name in GROUP_STATISTICS:
            fields.append(f"{metric}_{statistic_name}")

    return fields


def run_fieldnames() -> list[str]:
    return [
        "file",
        "experiment",
        "message_count",
        *[f"{metric}_mean" for metric in TARGET_COLUMNS],
    ]


def write_csv(
    output_path: Path,
    fieldnames: list[str],
    rows: Iterable[dict[str, object]],
) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)

    with output_path.open("w", encoding="utf-8-sig", newline="") as file:
        writer = csv.DictWriter(file, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def write_run_summaries(
    output_path: Path,
    runs: Iterable[RunSummary],
) -> None:
    rows: list[dict[str, object]] = []

    for run in sorted(runs, key=lambda item: (item.experiment, item.relative_file)):
        row: dict[str, object] = {
            "file": run.relative_file,
            "experiment": run.experiment,
            "message_count": run.message_count,
        }

        for metric in TARGET_COLUMNS:
            row[f"{metric}_mean"] = normalize_number(run.metric_means[metric])

        rows.append(row)

    write_csv(output_path, run_fieldnames(), rows)


def main() -> int:
    args = parse_args()

    root_dir = args.root_dir.expanduser().resolve()
    output_file = args.output_file.expanduser().resolve()
    runs_output_file = (
        args.runs_out.expanduser().resolve()
        if args.runs_out is not None
        else None
    )

    if not root_dir.exists():
        print(f"[error] directory does not exist: {root_dir}", file=sys.stderr)
        return 1

    if not root_dir.is_dir():
        print(f"[error] --dir is not a directory: {root_dir}", file=sys.stderr)
        return 1

    excluded_paths = {output_file}
    if runs_output_file is not None:
        excluded_paths.add(runs_output_file)

    message_files = sorted(
        (
            path
            for path in root_dir.rglob("messages.csv")
            if path.is_file() and path.resolve() not in excluded_paths
        ),
        key=lambda path: path.relative_to(root_dir).as_posix(),
    )

    if not message_files:
        print(
            f"[error] no messages.csv files found under: {root_dir}",
            file=sys.stderr,
        )
        return 1

    runs: list[RunSummary] = []

    for csv_path in message_files:
        try:
            run = summarize_run(csv_path, root_dir)
        except (OSError, csv.Error, ValueError) as error:
            print(f"[warning] skipped {csv_path}: {error}", file=sys.stderr)
            continue

        if (
            args.expected_messages > 0
            and run.message_count != args.expected_messages
        ):
            print(
                f"[warning] {csv_path}: expected {args.expected_messages} "
                f"messages but found {run.message_count}",
                file=sys.stderr,
            )

        runs.append(run)

    if not runs:
        print(
            "[error] no valid messages.csv files could be summarized",
            file=sys.stderr,
        )
        return 1

    aggregate_rows = aggregate_runs(runs)
    write_csv(output_file, aggregate_fieldnames(), aggregate_rows)

    if runs_output_file is not None:
        write_run_summaries(runs_output_file, runs)

    print(
        f"Summarized {len(runs)} topology-instance run(s) into "
        f"{len(aggregate_rows)} experiment group(s)."
    )
    print(f"Aggregate output: {output_file}")

    if runs_output_file is not None:
        print(f"Run-level output: {runs_output_file}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())