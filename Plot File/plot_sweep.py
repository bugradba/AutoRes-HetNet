#!/usr/bin/env python3
"""Sweep CSV'sinden makale figürleri üretir (yalnızca matplotlib gerekir).

Kullanım:
    python plot_sweep.py sweep_results.csv <cikti_oneki>

Üretilenler:
    <onek>_cdf.png      : K başına panel — yakınsama-turu CDF'leri (N eğrileri).
                          Yakınsamayan koşu varsa oranı panel etiketinde belirtilir.
    <onek>_scaling.png  : msg/BS ve CONFLICT vs N (±%95 GA hata çubukları).
    <onek>_heatmap.png  : K×N sıfır-girişim oranı ısı haritası
                          (kromatik sayı bölgesinin haritası).
"""
import csv
import math
import sys
from collections import defaultdict

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt


def load(path):
    rows = []
    with open(path) as f:
        for r in csv.DictReader(f):
            rows.append({
                "K": int(r["K"]), "N": int(r["N"]),
                "converged": r["converged"] == "true",
                "rounds": float(r["conv_rounds"]),
                "msgs": float(r["msgs_per_bs"]),
                "conflicts": float(r["conflicts"]),
                "zero": r["zero_interf"] == "true",
            })
    if not rows:
        sys.exit("CSV boş ya da okunamadı: " + path)
    return rows


def mean_ci(xs):
    n = len(xs)
    if n == 0:
        return float("nan"), 0.0
    m = sum(xs) / n
    if n < 2:
        return m, 0.0
    var = sum((x - m) ** 2 for x in xs) / (n - 1)
    return m, 1.96 * math.sqrt(var) / math.sqrt(n)


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    rows, prefix = load(sys.argv[1]), sys.argv[2]

    Ks = sorted({r["K"] for r in rows})
    Ns = sorted({r["N"] for r in rows})
    cell = defaultdict(list)
    for r in rows:
        cell[(r["K"], r["N"])].append(r)

    # ---------- 1) Yakınsama-turu CDF'leri ----------
    fig, axes = plt.subplots(1, len(Ks), figsize=(4.2 * len(Ks), 3.6),
                             sharey=True, squeeze=False)
    for ax, K in zip(axes[0], Ks):
        for N in Ns:
            rs = cell.get((K, N), [])
            conv = sorted(r["rounds"] for r in rs if r["converged"])
            if not conv:
                continue
            xs, ys = [], []
            for i, v in enumerate(conv, 1):
                xs.append(v)
                ys.append(i / len(rs))  # payda TÜM koşular: dürüst CDF
            label = f"N={N}"
            nonconv = len(rs) - len(conv)
            if nonconv:
                label += f" ({100*nonconv/len(rs):.0f}% yok)"
            ax.step(xs, ys, where="post", label=label)
        ax.set_title(f"K = {K}")
        ax.set_xlabel("yakınsama (protokol turu)")
        ax.grid(alpha=0.3)
        ax.legend(fontsize=8)
    axes[0][0].set_ylabel("CDF")
    axes[0][0].set_ylim(0, 1.02)
    fig.suptitle("Yakınsama-turu CDF'leri")
    fig.tight_layout()
    fig.savefig(f"{prefix}_cdf.png", dpi=160)
    plt.close(fig)

    # ---------- 2) Ölçekleme: msg/BS ve CONFLICT vs N ----------
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(9.6, 3.8))
    for K in Ks:
        m_m, m_c, e_m, e_c, xs = [], [], [], [], []
        for N in Ns:
            rs = cell.get((K, N), [])
            if not rs:
                continue
            xs.append(N)
            mm, em = mean_ci([r["msgs"] for r in rs])
            mc, ec = mean_ci([r["conflicts"] for r in rs])
            m_m.append(mm); e_m.append(em)
            m_c.append(mc); e_c.append(ec)
        ax1.errorbar(xs, m_m, yerr=e_m, marker="o", capsize=3, label=f"K={K}")
        ax2.errorbar(xs, m_c, yerr=e_c, marker="s", capsize=3, label=f"K={K}")
    ax1.set_xlabel("N (istasyon)"); ax1.set_ylabel("mesaj / istasyon")
    ax2.set_xlabel("N (istasyon)"); ax2.set_ylabel("CONFLICT / koşu")
    for ax in (ax1, ax2):
        ax.grid(alpha=0.3); ax.legend(fontsize=8)
    fig.suptitle("Mesaj karmaşıklığı ve çekişme ölçeklemesi (±%95 GA)")
    fig.tight_layout()
    fig.savefig(f"{prefix}_scaling.png", dpi=160)
    plt.close(fig)

    # ---------- 3) Sıfır-girişim ısı haritası ----------
    grid = [[100 * sum(r["zero"] for r in cell.get((K, N), [])) /
             max(1, len(cell.get((K, N), []))) for N in Ns] for K in Ks]
    fig, ax = plt.subplots(figsize=(1.1 * len(Ns) + 2, 0.9 * len(Ks) + 1.6))
    im = ax.imshow(grid, cmap="YlGn", vmin=0, vmax=100, aspect="auto")
    ax.set_xticks(range(len(Ns)), [str(n) for n in Ns])
    ax.set_yticks(range(len(Ks)), [str(k) for k in Ks])
    ax.set_xlabel("N (istasyon)"); ax.set_ylabel("K (renk)")
    for i in range(len(Ks)):
        for j in range(len(Ns)):
            ax.text(j, i, f"{grid[i][j]:.0f}%", ha="center", va="center", fontsize=9)
    fig.colorbar(im, label="sıfır-girişim koşu oranı (%)")
    ax.set_title("Sıfır girişim = grafın kromatik sayısı ≤ K bölgesi")
    fig.tight_layout()
    fig.savefig(f"{prefix}_heatmap.png", dpi=160)
    plt.close(fig)

    print(f"Yazıldı: {prefix}_cdf.png, {prefix}_scaling.png, {prefix}_heatmap.png")


if __name__ == "__main__":
    main()
