#!/usr/bin/env python3

import argparse
import csv
import math
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path, PurePosixPath


TARGET_COLUMNS = [
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
]

STATISTICS = ("mean", "median", "max", "min")

# Example:
#   260718/ba-base-flooding-t105948/results/messages.csv
#                         ^^^^^^^^^^^^^^^^^^^^^^^
# becomes:
#   ba-base-flooding
AUTO_MERGE_PATTERN = re.compile(r"^(?P<name>.+)-t[^/]+$")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Recursively collect every messages.csv under a directory and "
            "write per-file summary statistics."
        )
    )
    parser.add_argument(
        "--dir",
        required=True,
        type=Path,
        dest="root_dir",
        help="Root directory to search recursively",
    )
    parser.add_argument(
        "--out",
        required=True,
        type=Path,
        dest="output_file",
        help="Output CSV file",
    )
    parser.add_argument(
        "--auto-merge",
        action="store_true",
        help=(
            "Merge repeated experiment runs by removing the leading path and "
            "the trailing '-t...' run suffix, then average every summary field"
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


def normalize_number(value: float) -> int | float:
    """
    Write mathematically integral values such as 100.0 as 100.
    Non-integral values remain floats.
    """
    if float(value).is_integer():
        return int(value)
    return value


def output_fieldnames() -> list[str]:
    return ["file"] + [
        f"{column}_{statistic_name}"
        for column in TARGET_COLUMNS
        for statistic_name in STATISTICS
    ]


def summarize_file(csv_path: Path, root_dir: Path) -> dict[str, object]:
    values: dict[str, list[float]] = {
        column: [] for column in TARGET_COLUMNS
    }

    with csv_path.open("r", encoding="utf-8-sig", newline="") as file:
        reader = csv.DictReader(file)

        if reader.fieldnames is None:
            raise ValueError("CSV header is missing")

        missing_columns = [
            column
            for column in TARGET_COLUMNS
            if column not in reader.fieldnames
        ]
        if missing_columns:
            raise ValueError(
                "required columns are missing: "
                + ", ".join(missing_columns)
            )

        for row_number, row in enumerate(reader, start=2):
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

    result: dict[str, object] = {
        "file": csv_path.relative_to(root_dir).as_posix()
    }

    for column in TARGET_COLUMNS:
        column_values = values[column]

        if not column_values:
            for statistic_name in STATISTICS:
                result[f"{column}_{statistic_name}"] = ""
            continue

        result[f"{column}_mean"] = normalize_number(
            statistics.fmean(column_values)
        )
        result[f"{column}_median"] = normalize_number(
            statistics.median(column_values)
        )
        result[f"{column}_max"] = normalize_number(max(column_values))
        result[f"{column}_min"] = normalize_number(min(column_values))

    return result


def auto_merge_key(file_path: str) -> str:
    """
    Extract a common experiment name from a summarized relative file path.

    Example:
        260718/ba-base-flooding-t105948/results/messages.csv
        -> ba-base-flooding

    The function searches every path component for a component ending in
    '-t...'. This allows arbitrary leading directories such as dates.
    """
    parts = PurePosixPath(file_path).parts

    for part in parts:
        match = AUTO_MERGE_PATTERN.fullmatch(part)
        if match:
            return match.group("name")

    # Fallback for paths that do not follow the expected '-t...' naming rule.
    # For the common '<experiment>/results/messages.csv' structure, choose the
    # experiment directory and preserve its full name.
    if len(parts) >= 3 and parts[-2] == "results":
        fallback = parts[-3]
    elif len(parts) >= 2:
        fallback = parts[-2]
    else:
        fallback = PurePosixPath(file_path).stem

    print(
        f"[warning] no '-t...' run suffix found in {file_path!r}; "
        f"using {fallback!r} as the merge key",
        file=sys.stderr,
    )
    return fallback


def merge_summaries(
    summaries: list[dict[str, object]],
    fieldnames: list[str],
) -> list[dict[str, object]]:
    """
    Group summaries by normalized experiment name and calculate the arithmetic
    mean of each existing summary field, including fields ending in _median,
    _max, and _min.
    """
    groups: dict[str, list[dict[str, object]]] = defaultdict(list)

    for summary in summaries:
        groups[auto_merge_key(str(summary["file"]))].append(summary)

    merged: list[dict[str, object]] = []

    for merge_key in sorted(groups):
        rows = groups[merge_key]
        merged_row: dict[str, object] = {"file": merge_key}

        for fieldname in fieldnames[1:]:
            field_values = [
                number
                for row in rows
                if (number := parse_number(row.get(fieldname, ""))) is not None
            ]

            if field_values:
                merged_row[fieldname] = normalize_number(
                    statistics.fmean(field_values)
                )
            else:
                merged_row[fieldname] = ""

        merged.append(merged_row)

    return merged


def main() -> int:
    args = parse_args()

    root_dir = args.root_dir.expanduser().resolve()
    output_file = args.output_file.expanduser().resolve()

    if not root_dir.exists():
        print(
            f"[error] directory does not exist: {root_dir}",
            file=sys.stderr,
        )
        return 1

    if not root_dir.is_dir():
        print(
            f"[error] --dir is not a directory: {root_dir}",
            file=sys.stderr,
        )
        return 1

    message_files = sorted(
        (
            path
            for path in root_dir.rglob("messages.csv")
            if path.is_file() and path.resolve() != output_file
        ),
        key=lambda path: path.relative_to(root_dir).as_posix(),
    )

    if not message_files:
        print(
            f"[error] no messages.csv files found under: {root_dir}",
            file=sys.stderr,
        )
        return 1

    summaries: list[dict[str, object]] = []

    for csv_path in message_files:
        try:
            summaries.append(summarize_file(csv_path, root_dir))
        except (OSError, csv.Error, ValueError) as error:
            print(
                f"[warning] skipped {csv_path}: {error}",
                file=sys.stderr,
            )

    if not summaries:
        print(
            "[error] no valid messages.csv files could be summarized",
            file=sys.stderr,
        )
        return 1

    fieldnames = output_fieldnames()
    original_summary_count = len(summaries)

    if args.auto_merge:
        summaries = merge_summaries(summaries, fieldnames)

    output_file.parent.mkdir(parents=True, exist_ok=True)

    with output_file.open("w", encoding="utf-8-sig", newline="") as file:
        writer = csv.DictWriter(file, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(summaries)

    if args.auto_merge:
        print(
            f"Summarized {original_summary_count} file(s), merged into "
            f"{len(summaries)} group(s), and wrote {output_file}"
        )
    else:
        print(
            f"Summarized {len(summaries)} file(s) into {output_file}"
        )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())