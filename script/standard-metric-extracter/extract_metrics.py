#!/usr/bin/env python3
"""
Generate ER / BA / WS graphs repeatedly and directly measure:

  1. Degree coefficient of variation (CV)
  2. Global clustering coefficient (transitivity)
  3. Global efficiency

The 3 topologies x 3 metrics give the requested 9 baseline values.
Runs are parallelized with ProcessPoolExecutor.

Examples:
    python topology_metrics.py -N 400 -k 12 -m 6 -b 0.1 -r 100
    python topology_metrics.py -N 400 -k 12 -b 0.1 -r 100 -w 32
    python topology_metrics.py -N 400 -k 12 -b 0.1 -r 100 --csv results.csv

Dependencies:
    pip install networkx
"""

from __future__ import annotations

import argparse
import csv
import math
import os
import statistics
import sys
from concurrent.futures import ProcessPoolExecutor
from dataclasses import dataclass
from typing import Iterable

import networkx as nx


TOPOLOGIES = ("ER", "BA", "WS")
METRICS = ("degree_cv", "transitivity", "global_efficiency")


@dataclass(frozen=True)
class Task:
    topology: str
    run: int
    seed: int
    n: int
    k: int
    m: int
    beta: float


@dataclass(frozen=True)
class Result:
    topology: str
    run: int
    seed: int
    edges: int
    mean_degree: float
    degree_cv: float
    transitivity: float
    global_efficiency: float


def degree_cv(g: nx.Graph) -> float:
    """Population coefficient of variation of node degree."""
    n = g.number_of_nodes()
    if n == 0:
        return math.nan

    degrees = [d for _, d in g.degree()]
    mean = sum(degrees) / n

    if mean == 0.0:
        return 0.0

    variance = sum((d - mean) ** 2 for d in degrees) / n
    return math.sqrt(variance) / mean


def generate_graph(task: Task) -> nx.Graph:
    if task.topology == "ER":
        # E[degree] = p * (N - 1) = k
        p = task.k / (task.n - 1)
        # Faster than gnp_random_graph for the sparse regime.
        return nx.fast_gnp_random_graph(
            n=task.n,
            p=p,
            seed=task.seed,
            directed=False,
        )

    if task.topology == "BA":
        return nx.barabasi_albert_graph(
            n=task.n,
            m=task.m,
            seed=task.seed,
        )

    if task.topology == "WS":
        return nx.watts_strogatz_graph(
            n=task.n,
            k=task.k,
            p=task.beta,
            seed=task.seed,
        )

    raise ValueError(f"Unknown topology: {task.topology}")


def run_task(task: Task) -> Result:
    g = generate_graph(task)

    n = g.number_of_nodes()
    edges = g.number_of_edges()
    mean_degree = (2.0 * edges / n) if n else math.nan

    return Result(
        topology=task.topology,
        run=task.run,
        seed=task.seed,
        edges=edges,
        mean_degree=mean_degree,
        degree_cv=degree_cv(g),
        transitivity=nx.transitivity(g),
        global_efficiency=nx.global_efficiency(g),
    )


def build_tasks(
    n: int,
    k: int,
    m: int,
    beta: float,
    runs: int,
    base_seed: int,
) -> list[Task]:
    tasks: list[Task] = []

    # Give every (topology, run) pair a deterministic, distinct seed.
    topology_offset = {"ER": 0, "BA": 1, "WS": 2}

    for run in range(runs):
        for topology in TOPOLOGIES:
            seed = base_seed + run * 3 + topology_offset[topology]
            tasks.append(
                Task(
                    topology=topology,
                    run=run,
                    seed=seed,
                    n=n,
                    k=k,
                    m=m,
                    beta=beta,
                )
            )

    return tasks


def summarize(results: Iterable[Result]) -> dict[str, dict[str, tuple[float, float]]]:
    grouped: dict[str, list[Result]] = {t: [] for t in TOPOLOGIES}

    for result in results:
        grouped[result.topology].append(result)

    summary: dict[str, dict[str, tuple[float, float]]] = {}

    for topology in TOPOLOGIES:
        rows = grouped[topology]
        summary[topology] = {}

        for metric in ("mean_degree",) + METRICS:
            values = [getattr(row, metric) for row in rows]
            mean = statistics.fmean(values)
            std = statistics.stdev(values) if len(values) > 1 else 0.0
            summary[topology][metric] = (mean, std)

    return summary


def print_summary(
    n: int,
    k: int,
    m: int,
    beta: float,
    runs: int,
    workers: int,
    summary: dict[str, dict[str, tuple[float, float]]],
) -> None:
    print(
        f"N={n}, k={k}, m={m}, beta={beta}, "
        f"runs={runs}, workers={workers}"
    )
    print()

    header = (
        f"{'Topology':<10}"
        f"{'Mean Degree':>22}"
        f"{'Degree CV':>22}"
        f"{'Transitivity':>22}"
        f"{'Global Efficiency':>22}"
    )
    print(header)
    print("-" * len(header))

    for topology in TOPOLOGIES:
        values = summary[topology]

        def fmt(metric: str) -> str:
            mean, std = values[metric]
            return f"{mean:.6f} ± {std:.6f}"

        print(
            f"{topology:<10}"
            f"{fmt('mean_degree'):>22}"
            f"{fmt('degree_cv'):>22}"
            f"{fmt('transitivity'):>22}"
            f"{fmt('global_efficiency'):>22}"
        )


def write_csv(path: str, results: list[Result]) -> None:
    with open(path, "w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerow(
            [
                "topology",
                "run",
                "seed",
                "edges",
                "mean_degree",
                "degree_cv",
                "transitivity",
                "global_efficiency",
            ]
        )

        for row in sorted(results, key=lambda x: (x.run, x.topology)):
            writer.writerow(
                [
                    row.topology,
                    row.run,
                    row.seed,
                    row.edges,
                    f"{row.mean_degree:.12g}",
                    f"{row.degree_cv:.12g}",
                    f"{row.transitivity:.12g}",
                    f"{row.global_efficiency:.12g}",
                ]
            )


def validate(n: int, k: int, m: int, beta: float, runs: int) -> None:
    if n < 3:
        raise ValueError("N must be >= 3")

    if not (0 < k < n):
        raise ValueError("k must satisfy 0 < k < N")

    if k % 2 != 0:
        raise ValueError(
            "k must be even because NetworkX Watts-Strogatz uses "
            "k nearest neighbors around the ring."
        )

    if not (1 <= m < n):
        raise ValueError("m must satisfy 1 <= m < N")

    if not (0.0 <= beta <= 1.0):
        raise ValueError("beta must be in [0, 1]")

    if runs < 1:
        raise ValueError("runs must be >= 1")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Generate ER/BA/WS topologies and directly measure "
            "Degree CV, transitivity, and global efficiency in parallel."
        )
    )

    parser.add_argument(
        "-N",
        "--nodes",
        type=int,
        required=True,
        help="Number of nodes",
    )
    parser.add_argument(
        "-k",
        "--degree",
        type=int,
        required=True,
        help=(
            "Target ER mean degree and WS ring degree. "
            "Must be even for WS."
        ),
    )
    parser.add_argument(
        "-m",
        type=int,
        default=None,
        help="BA attachment parameter. Default: k/2",
    )
    parser.add_argument(
        "-b",
        "--beta",
        type=float,
        default=0.1,
        help="WS rewiring probability (default: 0.1)",
    )
    parser.add_argument(
        "-r",
        "--runs",
        type=int,
        default=100,
        help="Number of graph realizations per topology (default: 100)",
    )
    parser.add_argument(
        "-w",
        "--workers",
        type=int,
        default=0,
        help=(
            "Number of worker processes. "
            "0 = os.cpu_count() (default: 0)"
        ),
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=42,
        help="Base random seed (default: 42)",
    )
    parser.add_argument(
        "--csv",
        type=str,
        default=None,
        help="Optional path for per-run CSV output",
    )

    return parser.parse_args()


def main() -> None:
    args = parse_args()

    n = args.nodes
    k = args.degree

    if args.m is None:
        if k % 2 != 0:
            raise ValueError("Cannot infer m=k/2 from odd k.")
        m = k // 2
    else:
        m = args.m

    validate(
        n=n,
        k=k,
        m=m,
        beta=args.beta,
        runs=args.runs,
    )

    workers = args.workers if args.workers > 0 else (os.cpu_count() or 1)

    if 2 * m != k:
        print(
            f"warning: BA has asymptotic mean degree ~2m={2*m}, "
            f"while ER/WS target k={k}. "
            f"For matched mean degree, use m={k/2:g}.",
            file=sys.stderr,
        )

    tasks = build_tasks(
        n=n,
        k=k,
        m=m,
        beta=args.beta,
        runs=args.runs,
        base_seed=args.seed,
    )

    # map() preserves input order and distributes independent realizations
    # across worker processes. chunksize > 1 reduces IPC overhead for many runs.
    chunksize = max(1, len(tasks) // max(1, workers * 4))

    with ProcessPoolExecutor(max_workers=workers) as executor:
        results = list(
            executor.map(
                run_task,
                tasks,
                chunksize=chunksize,
            )
        )

    stats = summarize(results)

    print_summary(
        n=n,
        k=k,
        m=m,
        beta=args.beta,
        runs=args.runs,
        workers=workers,
        summary=stats,
    )

    if args.csv:
        write_csv(args.csv, results)
        print()
        print(f"Per-run CSV written to: {args.csv}")


if __name__ == "__main__":
    main()