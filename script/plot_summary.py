#!/usr/bin/env python3

import argparse
import sys
from pathlib import Path

import matplotlib.pyplot as plt
import pandas as pd


DEFAULT_METRICS = [
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

DEFAULT_STATS = ["mean", "median", "max", "min"]

METRIC_TITLES = {
    "reached_nodes": "Reached Nodes",
    "total_nodes": "Total Nodes",
    "reachability": "Reachability",
    "completion_delay_ms": "Completion Delay (ms)",
    "transmissions": "Transmissions",
    "duplicates": "Duplicates",
    "drops": "Drops",
    "suppressions": "Suppressions",
    "control_sent": "Control Sent",
    "control_received": "Control Received",
    "control_drops": "Control Drops",
    "control_bytes_sent": "Control Bytes Sent",
}

STAT_LABELS = {
    "mean": "Mean",
    "median": "Median",
    "max": "Max",
    "min": "Min",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Create line charts from an auto-merged summary CSV. "
            "One PNG is generated per metric."
        )
    )
    parser.add_argument(
        "--in",
        dest="input_csv",
        required=True,
        type=Path,
        help="Input merged summary CSV",
    )
    parser.add_argument(
        "--out-dir",
        required=True,
        type=Path,
        help="Directory in which PNG charts are saved",
    )
    parser.add_argument(
        "--metrics",
        default="all",
        help=(
            "Comma-separated metrics to plot, or 'all'. "
            "Example: completion_delay_ms,transmissions,duplicates"
        ),
    )
    parser.add_argument(
        "--stats",
        default=",".join(DEFAULT_STATS),
        help="Comma-separated statistics: mean,median,max,min",
    )
    parser.add_argument(
        "--dpi",
        type=int,
        default=200,
        help="Output image DPI",
    )
    parser.add_argument(
        "--width",
        type=float,
        default=13.0,
        help="Figure width in inches",
    )
    parser.add_argument(
        "--height",
        type=float,
        default=7.0,
        help="Figure height in inches",
    )
    parser.add_argument(
        "--sort",
        choices=["input", "name"],
        default="input",
        help="Order of experiment names on the x-axis",
    )
    parser.add_argument(
        "--log-scale",
        action="store_true",
        help="Use logarithmic y-axis where all plotted values are positive",
    )
    parser.add_argument(
        "--show-values",
        action="store_true",
        help="Show numeric values next to points",
    )
    return parser.parse_args()


def parse_csv_option(value: str) -> list[str]:
    return [item.strip() for item in value.split(",") if item.strip()]


def validate_requested_stats(stats: list[str]) -> None:
    invalid = [stat for stat in stats if stat not in DEFAULT_STATS]
    if invalid:
        raise ValueError(
            "unsupported statistics: " + ", ".join(invalid)
            + "; allowed: " + ", ".join(DEFAULT_STATS)
        )


def available_metrics(df: pd.DataFrame) -> list[str]:
    result = []
    for metric in DEFAULT_METRICS:
        if any(f"{metric}_{stat}" in df.columns for stat in DEFAULT_STATS):
            result.append(metric)
    return result


def plot_metric(
    df: pd.DataFrame,
    metric: str,
    stats: list[str],
    output_file: Path,
    dpi: int,
    width: float,
    height: float,
    log_scale: bool,
    show_values: bool,
) -> None:
    columns = [f"{metric}_{stat}" for stat in stats]
    existing = [column for column in columns if column in df.columns]

    if not existing:
        print(
            f"[warning] skipped {metric}: no matching columns",
            file=sys.stderr,
        )
        return

    x_labels = df["file"].astype(str).tolist()
    x_positions = list(range(len(x_labels)))

    fig, ax = plt.subplots(figsize=(width, height))

    all_positive = True

    for stat in stats:
        column = f"{metric}_{stat}"
        if column not in df.columns:
            print(
                f"[warning] missing column: {column}",
                file=sys.stderr,
            )
            continue

        values = pd.to_numeric(df[column], errors="coerce")

        valid_values = values.dropna()
        if not valid_values.empty and (valid_values <= 0).any():
            all_positive = False

        ax.plot(
            x_positions,
            values,
            marker="o",
            linewidth=1.8,
            markersize=5,
            label=STAT_LABELS.get(stat, stat),
        )

        if show_values:
            for x, value in zip(x_positions, values):
                if pd.isna(value):
                    continue
                ax.annotate(
                    f"{value:.3f}".rstrip("0").rstrip("."),
                    (x, value),
                    xytext=(0, 6),
                    textcoords="offset points",
                    ha="center",
                    fontsize=7,
                )

    title = METRIC_TITLES.get(metric, metric)
    ax.set_title(title, fontsize=15, fontweight="bold")
    ax.set_xlabel("Experiment")
    ax.set_ylabel(title)
    ax.set_xticks(x_positions)
    ax.set_xticklabels(x_labels, rotation=35, ha="right")
    ax.grid(True, alpha=0.3)
    ax.legend()

    if log_scale:
        if all_positive:
            ax.set_yscale("log")
        else:
            print(
                f"[warning] {metric}: log scale disabled because "
                "zero or negative values exist",
                file=sys.stderr,
            )

    fig.tight_layout()
    output_file.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(output_file, dpi=dpi, bbox_inches="tight")
    plt.close(fig)

    print(f"saved: {output_file}")


def main() -> int:
    args = parse_args()

    input_csv = args.input_csv.expanduser().resolve()
    output_dir = args.out_dir.expanduser().resolve()

    if not input_csv.exists():
        print(f"[error] input CSV not found: {input_csv}", file=sys.stderr)
        return 1

    try:
        df = pd.read_csv(input_csv)
    except Exception as error:
        print(f"[error] failed to read CSV: {error}", file=sys.stderr)
        return 1

    if "file" not in df.columns:
        print("[error] input CSV must contain a 'file' column", file=sys.stderr)
        return 1

    if args.sort == "name":
        df = df.sort_values("file", kind="stable").reset_index(drop=True)

    requested_stats = parse_csv_option(args.stats)
    try:
        validate_requested_stats(requested_stats)
    except ValueError as error:
        print(f"[error] {error}", file=sys.stderr)
        return 1

    detected_metrics = available_metrics(df)

    if args.metrics.strip().lower() == "all":
        requested_metrics = detected_metrics
    else:
        requested_metrics = parse_csv_option(args.metrics)

    if not requested_metrics:
        print("[error] no metrics selected", file=sys.stderr)
        return 1

    unknown_metrics = [
        metric for metric in requested_metrics
        if metric not in detected_metrics
    ]
    if unknown_metrics:
        print(
            "[warning] unavailable metrics: " + ", ".join(unknown_metrics),
            file=sys.stderr,
        )

    output_dir.mkdir(parents=True, exist_ok=True)

    generated = 0
    for index, metric in enumerate(requested_metrics, start=1):
        if metric not in detected_metrics:
            continue

        output_file = output_dir / f"{index:02d}_{metric}.png"
        plot_metric(
            df=df,
            metric=metric,
            stats=requested_stats,
            output_file=output_file,
            dpi=args.dpi,
            width=args.width,
            height=args.height,
            log_scale=args.log_scale,
            show_values=args.show_values,
        )
        generated += 1

    if generated == 0:
        print("[error] no charts were generated", file=sys.stderr)
        return 1

    print(f"generated {generated} chart(s) in {output_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())