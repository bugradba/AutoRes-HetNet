#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
plot_sweep.py — AutoRes-HetNet yakınsama taraması grafikleri

Kullanım:
    python plot_sweep.py [sweep_results.csv] [çıktı_öneki]

Girdi : go run . -sweep ... komutunun ürettiği CSV
Çıktı : <önek>_cdf.png        yakınsama-turu CDF'leri (K başına panel, N eğrileri)
        <önek>_scaling.png    msg/BS ve CONFLICT'in N ile ölçeklenmesi (ort ± %95 GA)
        <önek>_zerointerf.png sıfır-girişim oranı ısı haritası (K x N)

Bağımlılık: yalnızca matplotlib (pip install matplotlib). pandas GEREKMEZ.
"""
import csv
import math
import sys
from collections import defaultdict

import matplotlib

matplotlib.use("Agg")  # ekransız ortamlar için
import matplotlib.pyplot as plt

CSV_PATH = sys.argv[1] if len(sys.argv) > 1 else "sweep_results.csv"
PREFIX = sys.argv[2] if len(sys.argv) > 2 else "sweep"


def load(path):
    rows = []
    with open(path, newline="", encoding="utf-8") as f:
        for r in csv.DictReader(f):
            rows.append(
                dict(
                    K=int(r["K"]),
                    N=int(r["N"]),
                    converged=r["converged"] == "true",
                    rounds=float(r["conv_rounds"]),
                    msgs=float(r["msgs_per_bs"]),
                    conflicts=float(r["conflicts"]),
                    zero=r["zero_interf"] == "true",
                )
            )
    return rows


def mean_ci(xs):
    n = len(xs)
    if n == 0:
        return float("nan"), 0.0
    m = sum(xs) / n
    if n < 2:
        return m, 0.0
    sd = math.sqrt(sum((x - m) ** 2 for x in xs) / (n - 1))
    return m, 1.96 * sd / math.sqrt(n)


rows = load(CSV_PATH)
Ks = sorted({r["K"] for r in rows})
Ns = sorted({r["N"] for r in rows})
by_kn = defaultdict(list)
for r in rows:
    by_kn[(r["K"], r["N"])].append(r)

print(f"{CSV_PATH}: {len(rows)} koşu, K={Ks}, N={Ns}")

# ---------- 1) Yakınsama-turu CDF'leri ----------
# Makalenin ana grafiği: her K paneli içinde N eğrileri.
# YALNIZCA yakınsayan koşular çizilir; yakınsamayanların oranı
# eğri etiketinde ayrıca raporlanır (sansürlü veri gizlenmez).
fig, axes = plt.subplots(1, len(Ks), figsize=(4 * len(Ks), 3.4), sharey=True)
if len(Ks) == 1:
    axes = [axes]
for ax, K in zip(axes, Ks):
    for N in Ns:
        cell = by_kn.get((K, N), [])
        conv = sorted(r["rounds"] for r in cell if r["converged"])
        if not cell:
            continue
        rate = len(conv) / len(cell)
        label = f"N={N}" + ("" if rate == 1.0 else f" (conv {rate:.0%})")
        if conv:
            ys = [(i + 1) / len(conv) for i in range(len(conv))]
            ax.step(conv, ys, where="post", label=label)
    ax.set_title(f"K = {K}")
    ax.set_xlabel("Yakınsama süresi (protokol turu)")
    ax.grid(alpha=0.3)
    ax.legend(fontsize=8)
axes[0].set_ylabel("Ampirik CDF")
fig.suptitle("Asenkron GCG: yakınsama-süresi dağılımları")
fig.tight_layout()
fig.savefig(f"{PREFIX}_cdf.png", dpi=150)
print(f"yazıldı: {PREFIX}_cdf.png")

# ---------- 2) Ölçekleme: msg/BS ve CONFLICT vs N ----------
fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(9, 3.6))
for K in Ks:
    ms, mcis, cs, ccis = [], [], [], []
    for N in Ns:
        cell = by_kn.get((K, N), [])
        m, ci = mean_ci([r["msgs"] for r in cell])
        ms.append(m)
        mcis.append(ci)
        c, cci = mean_ci([r["conflicts"] for r in cell])
        cs.append(c)
        ccis.append(cci)
    ax1.errorbar(Ns, ms, yerr=mcis, marker="o", capsize=3, label=f"K={K}")
    ax2.errorbar(Ns, cs, yerr=ccis, marker="s", capsize=3, label=f"K={K}")
ax1.set_xlabel("N (istasyon sayısı, sabit alan)")
ax1.set_ylabel("Mesaj / istasyon")
ax1.set_title("Mesaj karmaşıklığı ~ doğrusal")
ax2.set_xlabel("N")
ax2.set_ylabel("Toplam CONFLICT")
ax2.set_title("Çekişme: N ile süper-doğrusal, K ile azalır")
for ax in (ax1, ax2):
    ax.grid(alpha=0.3)
    ax.legend(fontsize=8)
fig.tight_layout()
fig.savefig(f"{PREFIX}_scaling.png", dpi=150)
print(f"yazıldı: {PREFIX}_scaling.png")

# ---------- 3) Sıfır-girişim ısı haritası ----------
grid = [[100.0 * sum(r["zero"] for r in by_kn.get((K, N), [])) / max(1, len(by_kn.get((K, N), []))) for N in Ns] for K in Ks]
fig, ax = plt.subplots(figsize=(1.1 * len(Ns) + 2, 0.8 * len(Ks) + 1.6))
im = ax.imshow(grid, aspect="auto", cmap="YlGn", vmin=0, vmax=100)
ax.set_xticks(range(len(Ns)), [str(n) for n in Ns])
ax.set_yticks(range(len(Ks)), [str(k) for k in Ks])
ax.set_xlabel("N")
ax.set_ylabel("K")
ax.set_title("Sıfır-girişimli koşu oranı (%)\n(K'nin kromatik sayıya yetme izi)")
for i in range(len(Ks)):
    for j in range(len(Ns)):
        ax.text(j, i, f"{grid[i][j]:.0f}", ha="center", va="center", fontsize=9)
fig.colorbar(im, ax=ax, shrink=0.8)
fig.tight_layout()
fig.savefig(f"{PREFIX}_zerointerf.png", dpi=150)
print(f"yazıldı: {PREFIX}_zerointerf.png")
