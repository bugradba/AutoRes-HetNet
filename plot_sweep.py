#!/usr/bin/env python3
"""plot_sweep.py — sweep_results.csv'den yakınsama grafikleri üretir.

Kullanım:
    python plot_sweep.py sweep_results.csv sweep

Üretilenler (yalnızca matplotlib gerekir, pandas gerekmez):
    <prefix>_cdf.png      — (K, N) hücresi başına yakınsama süresi CDF'leri
    <prefix>_scaling.png  — K başına ortalama yakınsama turu vs N
    <prefix>_zeroheat.png — sıfır-girişimli koşu oranı ısı haritası (K x N)
"""

import csv
import sys
from collections import defaultdict

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt


def load(path):
    rows = []
    with open(path, newline="") as f:
        for row in csv.DictReader(f):
            rows.append(
                {
                    "k": int(row["k"]),
                    "n": int(row["n"]),
                    "converged": row["converged"] == "true",
                    "rounds": float(row["conv_rounds"]),
                    "zero": row["zero_interference"] == "true",
                }
            )
    if not rows:
        sys.exit(f"HATA: {path} boş ya da başlıksız")
    return rows


def plot_cdfs(rows, prefix):
    cells = defaultdict(list)
    for r in rows:
        if r["converged"]:
            cells[(r["k"], r["n"])].append(r["rounds"])

    plt.figure(figsize=(8, 5))
    for (k, n), vals in sorted(cells.items()):
        vals = sorted(vals)
        ys = [(i + 1) / len(vals) for i in range(len(vals))]
        plt.step(vals, ys, where="post", label=f"K={k}, N={n}")
    plt.xlabel("Yakınsama süresi (protokol turu)")
    plt.ylabel("CDF")
    plt.title("Yakınsama süresi CDF'leri (yalnızca yakınsayan koşular)")
    plt.legend(fontsize=7, ncol=2)
    plt.grid(alpha=0.3)
    plt.tight_layout()
    plt.savefig(f"{prefix}_cdf.png", dpi=150)
    plt.close()


def plot_scaling(rows, prefix):
    acc = defaultdict(list)
    for r in rows:
        if r["converged"]:
            acc[(r["k"], r["n"])].append(r["rounds"])

    ks = sorted({k for k, _ in acc})
    plt.figure(figsize=(7, 5))
    for k in ks:
        ns = sorted(n for kk, n in acc if kk == k)
        means = [sum(acc[(k, n)]) / len(acc[(k, n)]) for n in ns]
        plt.plot(ns, means, marker="o", label=f"K={k}")
    plt.xlabel("N (istasyon sayısı)")
    plt.ylabel("Ortalama yakınsama (protokol turu)")
    plt.title("Yoğunluğa göre yakınsama ölçeklemesi")
    plt.legend()
    plt.grid(alpha=0.3)
    plt.tight_layout()
    plt.savefig(f"{prefix}_scaling.png", dpi=150)
    plt.close()


def plot_zero_heatmap(rows, prefix):
    total = defaultdict(int)
    zero = defaultdict(int)
    for r in rows:
        total[(r["k"], r["n"])] += 1
        if r["zero"]:
            zero[(r["k"], r["n"])] += 1

    ks = sorted({k for k, _ in total})
    ns = sorted({n for _, n in total})
    grid = [[100.0 * zero[(k, n)] / total[(k, n)] for n in ns] for k in ks]

    plt.figure(figsize=(6, 4.5))
    im = plt.imshow(grid, aspect="auto", origin="lower", cmap="viridis", vmin=0, vmax=100)
    plt.colorbar(im, label="Sıfır-girişimli koşu oranı (%)")
    plt.xticks(range(len(ns)), ns)
    plt.yticks(range(len(ks)), ks)
    plt.xlabel("N")
    plt.ylabel("K")
    plt.title("Sıfır girişim: grafın özelliği (K >= kromatik sayı)")
    for i, k in enumerate(ks):
        for j, n in enumerate(ns):
            plt.text(j, i, f"{grid[i][j]:.0f}", ha="center", va="center",
                     color="white" if grid[i][j] < 60 else "black", fontsize=8)
    plt.tight_layout()
    plt.savefig(f"{prefix}_zeroheat.png", dpi=150)
    plt.close()


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    csv_path, prefix = sys.argv[1], sys.argv[2]
    rows = load(csv_path)
    plot_cdfs(rows, prefix)
    plot_scaling(rows, prefix)
    plot_zero_heatmap(rows, prefix)
    print(f"Yazıldı: {prefix}_cdf.png, {prefix}_scaling.png, {prefix}_zeroheat.png")


if __name__ == "__main__":
    main()
