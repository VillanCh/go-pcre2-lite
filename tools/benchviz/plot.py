#!/usr/bin/env python3
"""Generate Nature-style benchmark figures for go-pcre2-lite.

Data are the measured `go test -bench` results on darwin/arm64 (Apple M-series),
ns/op and allocs/op. Re-run the benchmarks and update the dicts below to refresh.

Usage:
    python3 tools/benchviz/plot.py
Outputs PNGs into assets/.
"""
import os
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np

# ---- Nature-ish styling -----------------------------------------------------
plt.rcParams.update({
    "font.family": "sans-serif",
    "font.sans-serif": ["Helvetica", "Arial", "DejaVu Sans"],
    "font.size": 9,
    "axes.linewidth": 0.8,
    "axes.spines.top": False,
    "axes.spines.right": False,
    "axes.titlesize": 11,
    "axes.titleweight": "bold",
    "axes.labelsize": 9.5,
    "xtick.direction": "out",
    "ytick.direction": "out",
    "legend.frameon": False,
    "legend.fontsize": 8.5,
    "figure.dpi": 200,
    "savefig.dpi": 200,
})

# Colorblind-friendly palette.
C_STD = "#9AA0A6"   # Go std regexp (RE2)
C_DL = "#5F6368"    # dlclark/regexp2 (engine we replace)
C_P2 = "#3B6FB6"    # go-pcre2-lite drop-in (regexp2 compat)
C_LL = "#E08214"    # go-pcre2-lite low-level (byte API)
C_BEFORE = "#C0392B"
C_AFTER = "#2E8B57"

ASSETS = os.path.join(os.path.dirname(__file__), "..", "..", "assets")
os.makedirs(ASSETS, exist_ok=True)


def out(name):
    return os.path.join(ASSETS, name)


def fmt_time(ns):
    if ns >= 1e6:
        return f"{ns/1e6:.1f} ms"
    if ns >= 1e3:
        return f"{ns/1e3:.1f} \u00b5s"
    return f"{ns:.0f} ns"


# ---- Measured data (ns/op) --------------------------------------------------
# Measured on darwin/arm64 (Apple M-series) against vendored PCRE2 10.47 with
# `go test -bench -benchmem`. scenario -> {engine: ns/op}; None means not
# applicable for that engine.
LAT = {
    "Boolean match\n(short)":        {"dl": 6472, "p2": 676,  "ll": 674,  "std": 716},
    "Find with\ncaptures":           {"dl": 1072, "p2": 984,  "ll": 690,  "std": 489},
    "Single match\n(100 KB)":        {"dl": 26400000, "p2": 2844000, "ll": 2840000, "std": 2926000},
    "Find-all\n(670 matches)":       {"dl": 380181, "p2": 137899, "ll": 62859, "std": 98894},
    "Find-all\n(30k matches)":       {"dl": 6058824, "p2": 5240747, "ll": 2875660, "std": 5495268},
}

ALLOCS = {
    "Find-all\n(670 matches)":  {"dl": 3326,   "p2": 2004,  "ll": 14,  "std": 672},
    "Find-all\n(30k matches)":  {"dl": 150001, "p2": 90124, "ll": 142, "std": 30028},
}

# Before/after the batched FindAll + NO_UTF_CHECK optimization (ns/op). The
# "before" figures are the historical un-batched path; "after" are the current
# PCRE2 10.47 measurements.
TINY = {
    "Find-all\n670 matches\n(4 KB)": {
        "p2_before": 680000, "p2_after": 137899,
        "ll_before": 589000, "ll_after": 62859,
        "dl": 380181, "std": 98894,
    },
    "Find-all\n30,000 matches\n(30 KB)": {
        "p2_before": 170000000, "p2_after": 5240747,
        "ll_before": 171000000, "ll_after": 2875660,
        "dl": 6058824, "std": 5495268,
    },
}

ENGINES = [("std", "Go std (RE2)", C_STD),
           ("dl", "dlclark/regexp2", C_DL),
           ("p2", "pcre2-lite (drop-in)", C_P2),
           ("ll", "pcre2-lite (low-level)", C_LL)]


def grouped_bars(data, ylabel, title, fname, logy=True, value_fmt=None):
    scenarios = list(data.keys())
    n_eng = len(ENGINES)
    x = np.arange(len(scenarios))
    width = 0.20
    fig, ax = plt.subplots(figsize=(9.0, 4.6))
    for i, (key, label, color) in enumerate(ENGINES):
        vals = [data[s].get(key) for s in scenarios]
        xs = x + (i - (n_eng - 1) / 2) * width
        heights = [v if v else np.nan for v in vals]
        bars = ax.bar(xs, heights, width, label=label, color=color,
                      edgecolor="white", linewidth=0.5)
        if value_fmt:
            for rect, v in zip(bars, vals):
                if v:
                    ax.annotate(value_fmt(v),
                                xy=(rect.get_x() + rect.get_width() / 2, v),
                                xytext=(0, 2), textcoords="offset points",
                                ha="center", va="bottom", fontsize=6.3, rotation=90,
                                color="#333333")
    if logy:
        ax.set_yscale("log")
        ax.set_ylim(top=ax.get_ylim()[1] * 4.0)  # headroom for rotated labels
    ax.set_xticks(x)
    ax.set_xticklabels(scenarios)
    ax.set_ylabel(ylabel)
    ax.set_title(title, pad=10)
    ax.legend(loc="center left", bbox_to_anchor=(1.005, 0.5), handlelength=1.2)
    ax.grid(axis="y", linewidth=0.4, alpha=0.35)
    ax.set_axisbelow(True)
    fig.savefig(out(fname), bbox_inches="tight")
    plt.close(fig)
    print("wrote", fname)


def speedup_fig():
    # speedup vs dlclark (>1 means faster than dlclark).
    scenarios = list(LAT.keys())
    y = np.arange(len(scenarios))
    h = 0.36
    fig, ax = plt.subplots(figsize=(8.6, 4.6))
    p2 = [LAT[s]["dl"] / LAT[s]["p2"] for s in scenarios]
    ll = [LAT[s]["dl"] / LAT[s]["ll"] for s in scenarios]
    b1 = ax.barh(y + h / 2, p2, h, label="pcre2-lite (drop-in)", color=C_P2,
                 edgecolor="white", linewidth=0.5)
    b2 = ax.barh(y - h / 2, ll, h, label="pcre2-lite (low-level)", color=C_LL,
                 edgecolor="white", linewidth=0.5)
    ax.axvline(1.0, color=C_DL, linewidth=1.0, linestyle="--")
    ax.text(1.02, len(scenarios) - 0.4, "dlclark/regexp2 = 1.0",
            color=C_DL, fontsize=8, va="center")
    for bars in (b1, b2):
        for rect in bars:
            w = rect.get_width()
            ax.annotate(f"{w:.1f}\u00d7", xy=(w, rect.get_y() + rect.get_height() / 2),
                        xytext=(3, 0), textcoords="offset points",
                        ha="left", va="center", fontsize=7.5, color="#333333")
    ax.set_yticks(y)
    ax.set_yticklabels([s.replace("\n", " ") for s in scenarios])
    ax.set_xlabel("Speedup vs dlclark/regexp2  (higher is better)")
    ax.set_title("go-pcre2-lite throughput vs dlclark/regexp2", pad=10)
    ax.legend(loc="center left", bbox_to_anchor=(1.005, 0.5))
    ax.grid(axis="x", linewidth=0.4, alpha=0.35)
    ax.set_axisbelow(True)
    ax.set_xlim(0, max(max(p2), max(ll)) * 1.15)
    fig.savefig(out("bench-speedup.png"), bbox_inches="tight")
    plt.close(fig)
    print("wrote bench-speedup.png")


def tiny_fig():
    scenarios = list(TINY.keys())
    x = np.arange(len(scenarios))
    width = 0.18
    series = [
        ("ll_before", "low-level (before)", C_BEFORE, 0.55),
        ("p2_before", "drop-in (before)", C_BEFORE, 1.0),
        ("dl", "dlclark", C_DL, 1.0),
        ("std", "Go std", C_STD, 1.0),
        ("p2_after", "drop-in (after)", C_AFTER, 1.0),
        ("ll_after", "low-level (after)", C_AFTER, 0.55),
    ]
    fig, ax = plt.subplots(figsize=(9.2, 4.8))
    n = len(series)
    for i, (key, label, color, alpha) in enumerate(series):
        vals = [TINY[s][key] for s in scenarios]
        xs = x + (i - (n - 1) / 2) * width
        bars = ax.bar(xs, vals, width, label=label, color=color, alpha=alpha,
                      edgecolor="white", linewidth=0.5)
        for rect, v in zip(bars, vals):
            ax.annotate(fmt_time(v), xy=(rect.get_x() + rect.get_width() / 2, v),
                        xytext=(0, 2), textcoords="offset points", ha="center",
                        va="bottom", fontsize=6.0, rotation=90, color="#333333")
    ax.set_yscale("log")
    ax.set_ylim(top=ax.get_ylim()[1] * 6.0)  # headroom for rotated labels
    ax.set_xticks(x)
    ax.set_xticklabels(scenarios)
    ax.set_ylabel("Time per operation (ns/op, log scale)")
    ax.set_title("Many small matches: batched FindAll + NO_UTF_CHECK (lower is better)",
                 pad=10)
    ax.legend(loc="center left", bbox_to_anchor=(1.005, 0.5), handlelength=1.2)
    ax.grid(axis="y", linewidth=0.4, alpha=0.35)
    ax.set_axisbelow(True)
    fig.savefig(out("bench-tiny-optimization.png"), bbox_inches="tight")
    plt.close(fig)
    print("wrote bench-tiny-optimization.png")


if __name__ == "__main__":
    speedup_fig()
    grouped_bars(LAT, "Time per operation (ns/op, log scale)",
                 "Match latency across scenarios (lower is better)",
                 "bench-latency.png", logy=True, value_fmt=fmt_time)
    grouped_bars(ALLOCS, "Allocations per operation (log scale)",
                 "Heap allocations per operation (lower is better)",
                 "bench-allocs.png", logy=True,
                 value_fmt=lambda v: f"{int(v)}")
    tiny_fig()
    print("done ->", os.path.normpath(ASSETS))
