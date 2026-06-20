#!/usr/bin/env python3
"""Generate benchmark graphs from the CSV files produced by `go run ./benchmarks`.

Usage:
    python3 benchmarks/plot.py [results_dir]

Reads results.csv and failover.csv from results_dir (default: benchmarks/results)
and writes PNG charts into <results_dir>/img/.

Requires: matplotlib (pip install matplotlib).
"""
import csv
import os
import sys

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

RESULTS_DIR = sys.argv[1] if len(sys.argv) > 1 else os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "results"
)
IMG_DIR = os.path.join(RESULTS_DIR, "img")
os.makedirs(IMG_DIR, exist_ok=True)


def load_results():
    rows = []
    with open(os.path.join(RESULTS_DIR, "results.csv")) as f:
        for r in csv.DictReader(f):
            for k in ("cluster_size", "concurrency", "count", "errors"):
                r[k] = int(r[k])
            for k in ("duration_sec", "throughput_ops_sec", "mean_ms",
                      "p50_ms", "p95_ms", "p99_ms", "max_ms"):
                r[k] = float(r[k])
            rows.append(r)
    return rows


def load_failover():
    rows = []
    path = os.path.join(RESULTS_DIR, "failover.csv")
    if not os.path.exists(path):
        return rows
    with open(path) as f:
        for r in csv.DictReader(f):
            r["trial"] = int(r["trial"])
            r["recovery_ms"] = float(r["recovery_ms"])
            rows.append(r)
    return rows


def filt(rows, **kw):
    out = []
    for r in rows:
        if all(r[k] == v for k, v in kw.items()):
            out.append(r)
    return sorted(out, key=lambda x: x["concurrency"])


def save(fig, name):
    path = os.path.join(IMG_DIR, name)
    fig.tight_layout()
    fig.savefig(path, dpi=130)
    plt.close(fig)
    print("wrote", path)


def plot_throughput(rows):
    w = filt(rows, experiment="write_sweep")
    rd = filt(rows, experiment="read_sweep")
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(11, 4.2))

    ax1.plot([r["concurrency"] for r in w], [r["throughput_ops_sec"] for r in w],
             "o-", color="#c0392b", label="write (PUT)")
    ax1.set_title("Write throughput vs concurrency\n(3-node cluster, writes to leader)")
    ax1.set_xlabel("concurrent clients")
    ax1.set_ylabel("throughput (ops/sec)")
    ax1.grid(True, alpha=0.3)
    ax1.legend()

    ax2.plot([r["concurrency"] for r in rd], [r["throughput_ops_sec"] for r in rd],
             "s-", color="#2980b9", label="read (GET)")
    ax2.set_title("Read throughput vs concurrency\n(3-node cluster, reads from leader)")
    ax2.set_xlabel("concurrent clients")
    ax2.set_ylabel("throughput (ops/sec)")
    ax2.grid(True, alpha=0.3)
    ax2.legend()
    save(fig, "throughput_vs_concurrency.png")


def plot_latency(rows, experiment, op_label, color, fname):
    s = filt(rows, experiment=experiment)
    x = [r["concurrency"] for r in s]
    fig, ax = plt.subplots(figsize=(7, 4.5))
    ax.plot(x, [r["p50_ms"] for r in s], "o-", label="p50", color="#27ae60")
    ax.plot(x, [r["p95_ms"] for r in s], "s-", label="p95", color="#e67e22")
    ax.plot(x, [r["p99_ms"] for r in s], "^-", label="p99", color="#c0392b")
    ax.set_title(f"{op_label} latency percentiles vs concurrency\n(3-node cluster)")
    ax.set_xlabel("concurrent clients")
    ax.set_ylabel("latency (ms)")
    ax.grid(True, alpha=0.3)
    ax.legend()
    save(fig, fname)


def plot_routing(rows):
    r = filt(rows, experiment="routing")
    by_target = {x["target"]: x for x in r}
    targets = [t for t in ("leader", "follower") if t in by_target]
    if not targets:
        return
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10, 4.2))
    colors = ["#16a085", "#8e44ad"]
    ax1.bar(targets, [by_target[t]["throughput_ops_sec"] for t in targets], color=colors)
    ax1.set_title("Write throughput by request target\n(3-node, 16 clients)")
    ax1.set_ylabel("throughput (ops/sec)")
    for i, t in enumerate(targets):
        ax1.text(i, by_target[t]["throughput_ops_sec"], f'{by_target[t]["throughput_ops_sec"]:.0f}',
                 ha="center", va="bottom")

    width = 0.35
    idx = range(len(targets))
    ax2.bar([i - width / 2 for i in idx], [by_target[t]["p50_ms"] for t in targets],
            width, label="p50", color="#27ae60")
    ax2.bar([i + width / 2 for i in idx], [by_target[t]["p99_ms"] for t in targets],
            width, label="p99", color="#c0392b")
    ax2.set_xticks(list(idx))
    ax2.set_xticklabels(targets)
    ax2.set_title("Write latency by request target\n(3-node, 16 clients)")
    ax2.set_ylabel("latency (ms)")
    ax2.legend()
    save(fig, "routing_comparison.png")


def plot_cluster_size(rows):
    r = sorted(filt(rows, experiment="cluster_size"), key=lambda x: x["cluster_size"])
    if not r:
        return
    labels = [f'{x["cluster_size"]} nodes' for x in r]
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(10, 4.2))
    ax1.bar(labels, [x["throughput_ops_sec"] for x in r], color="#2c3e50")
    ax1.set_title("Write throughput vs cluster size\n(16 clients, writes to leader)")
    ax1.set_ylabel("throughput (ops/sec)")
    for i, x in enumerate(r):
        ax1.text(i, x["throughput_ops_sec"], f'{x["throughput_ops_sec"]:.0f}', ha="center", va="bottom")

    width = 0.35
    idx = range(len(r))
    ax2.bar([i - width / 2 for i in idx], [x["p50_ms"] for x in r], width, label="p50", color="#27ae60")
    ax2.bar([i + width / 2 for i in idx], [x["p99_ms"] for x in r], width, label="p99", color="#c0392b")
    ax2.set_xticks(list(idx))
    ax2.set_xticklabels(labels)
    ax2.set_title("Write latency vs cluster size\n(16 clients)")
    ax2.set_ylabel("latency (ms)")
    ax2.legend()
    save(fig, "cluster_size_comparison.png")


def plot_failover(rows):
    if not rows:
        return
    fig, ax = plt.subplots(figsize=(7, 4.2))
    labels = [f'trial {r["trial"]}\n{r["old_leader"]}\u2192{r["new_leader"]}' for r in rows]
    vals = [r["recovery_ms"] for r in rows]
    ax.bar(labels, vals, color="#d35400")
    avg = sum(vals) / len(vals)
    ax.axhline(avg, ls="--", color="#2c3e50", label=f"mean {avg:.0f} ms")
    for i, v in enumerate(vals):
        ax.text(i, v, f"{v:.0f} ms", ha="center", va="bottom")
    ax.set_title("Write availability: recovery time after leader crash\n(3-node cluster)")
    ax.set_ylabel("time to first committed write (ms)")
    ax.legend()
    save(fig, "failover_recovery.png")


def main():
    rows = load_results()
    plot_throughput(rows)
    plot_latency(rows, "write_sweep", "Write (PUT)", "#c0392b", "write_latency_percentiles.png")
    plot_latency(rows, "read_sweep", "Read (GET)", "#2980b9", "read_latency_percentiles.png")
    plot_routing(rows)
    plot_cluster_size(rows)
    plot_failover(load_failover())


if __name__ == "__main__":
    main()
