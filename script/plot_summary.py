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


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Create box plots from an auto-merged summary CSV. "
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
        help="Show summary values next to each box plot",
    )
    return parser.parse_args()


def parse_csv_option(value: str) -> list[str]:
    return [item.strip() for item in value.split(",") if item.strip()]


def available_metrics(df: pd.DataFrame) -> list[str]:
    result = []

    for metric in DEFAULT_METRICS:
        required = [
            f"{metric}_mean",
            f"{metric}_median",
            f"{metric}_q1",
            f"{metric}_q3",
            f"{metric}_min",
            f"{metric}_max",
        ]

        if all(column in df.columns for column in required):
            result.append(metric)

    return result


def plot_metric(
    df: pd.DataFrame,
    metric: str,
    output_file: Path,
    dpi: int,
    width: float,
    height: float,
    log_scale: bool,
    show_values: bool,
) -> None:
    required_columns = {
        "mean": f"{metric}_mean",
        "median": f"{metric}_median",
        "q1": f"{metric}_q1",
        "q3": f"{metric}_q3",
        "min": f"{metric}_min",
        "max": f"{metric}_max",
    }

    missing_columns = [
        column
        for column in required_columns.values()
        if column not in df.columns
    ]

    if missing_columns:
        print(
            f"[warning] skipped {metric}: missing columns: "
            + ", ".join(missing_columns),
            file=sys.stderr,
        )
        return

    # Convert all required columns to numeric
    mean_values = pd.to_numeric(df[required_columns["mean"]], errors="coerce")
    median_values = pd.to_numeric(df[required_columns["median"]], errors="coerce")
    q1_values = pd.to_numeric(df[required_columns["q1"]], errors="coerce")
    q3_values = pd.to_numeric(df[required_columns["q3"]], errors="coerce")
    min_values = pd.to_numeric(df[required_columns["min"]], errors="coerce")
    max_values = pd.to_numeric(df[required_columns["max"]], errors="coerce")

    fig, ax = plt.subplots(figsize=(width, height))

    labels = df["file"].astype(str).tolist()
    bxp_stats = []

    all_values_for_log_check = []

    for label, mean, median, q1, q3, min_, max_ in zip(
        labels,
        mean_values,
        median_values,
        q1_values,
        q3_values,
        min_values,
        max_values,
    ):
        values = [mean, median, q1, q3, min_, max_]

        if any(pd.isna(v) for v in values):
            print(
                f"[warning] skipped one row in {metric}: NaN detected for {label}",
                file=sys.stderr,
            )
            continue

        # Basic consistency checks
        if not (min_ <= q1 <= median <= q3 <= max_):
            print(
                f"[warning] inconsistent box stats for {metric} / {label}: "
                f"min={min_}, q1={q1}, median={median}, q3={q3}, max={max_}",
                file=sys.stderr,
            )
            continue

        bxp_stats.append({
            "label": label,
            "whislo": float(min_),     # bottom whisker
            "q1": float(q1),           # box bottom
            "med": float(median),      # median line
            "q3": float(q3),           # box top
            "whishi": float(max_),     # top whisker
            "mean": float(mean),       # mean point
            "fliers": [],              # no outlier data available
        })

        all_values_for_log_check.extend([mean, median, q1, q3, min_, max_])

    if not bxp_stats:
        print(
            f"[warning] skipped {metric}: no valid box plot rows",
            file=sys.stderr,
        )
        plt.close(fig)
        return

    ax.bxp(
        bxp_stats,
        showmeans=True,
        meanline=False,
        showfliers=False,
        vert=True,
        patch_artist=False,
    )

    if show_values:
        for x, stats_dict in enumerate(bxp_stats, start=1):
            mean = stats_dict["mean"]
            median = stats_dict["med"]
            q1 = stats_dict["q1"]
            q3 = stats_dict["q3"]
            min_ = stats_dict["whislo"]
            max_ = stats_dict["whishi"]

            ax.annotate(
                f"mean {mean:.3f}".rstrip("0").rstrip("."),
                (x, mean),
                xytext=(5, 0),
                textcoords="offset points",
                fontsize=7,
                va="center",
            )
            ax.annotate(
                f"median {median:.3f}".rstrip("0").rstrip("."),
                (x, median),
                xytext=(5, -10),
                textcoords="offset points",
                fontsize=7,
                va="center",
            )
            ax.annotate(
                f"q1 {q1:.3f}".rstrip("0").rstrip("."),
                (x, q1),
                xytext=(5, -10),
                textcoords="offset points",
                fontsize=7,
                va="center",
            )
            ax.annotate(
                f"q3 {q3:.3f}".rstrip("0").rstrip("."),
                (x, q3),
                xytext=(5, 0),
                textcoords="offset points",
                fontsize=7,
                va="center",
            )
            ax.annotate(
                f"min {min_:.3f}".rstrip("0").rstrip("."),
                (x, min_),
                xytext=(5, -2),
                textcoords="offset points",
                fontsize=7,
                va="top",
            )
            ax.annotate(
                f"max {max_:.3f}".rstrip("0").rstrip("."),
                (x, max_),
                xytext=(5, 2),
                textcoords="offset points",
                fontsize=7,
                va="bottom",
            )

    title = METRIC_TITLES.get(metric, metric)
    ax.set_title(title, fontsize=15, fontweight="bold")
    ax.set_xlabel("Experiment")
    ax.set_ylabel(title)
    ax.grid(True, axis="y", alpha=0.3)

    # x tick label rotation
    plt.setp(ax.get_xticklabels(), rotation=35, ha="right")

    if log_scale:
        if all(v > 0 for v in all_values_for_log_check):
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