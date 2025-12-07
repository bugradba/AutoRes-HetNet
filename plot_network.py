import json
import matplotlib.pyplot as plt
import networkx as nx
import numpy as np
from matplotlib.patches import Circle
import matplotlib.lines as mlines

def main():
    filename = 'viz_data.json'

    # Veriyi Yükle
    try:
        with open(filename, 'r') as f:
            data = json.load(f)
        print(f" '{filename}' başarıyla yüklendi.")
    except FileNotFoundError:
        print(f" HATA: '{filename}' bulunamadı. Lütfen önce Go simülasyonunu çalıştırın.")
        return

    # Grafik Ayarları
    plt.style.use('seaborn-v0_8-whitegrid') # Arka planı ızgaralı ve temiz yapar
    fig, ax = plt.subplots(figsize=(14, 12))

    # NetworkX Grafiğini Oluştur
    G = nx.Graph()
    pos = {}
    node_colors = []
    prb_map = {} # ID -> PRB

    # Renk Paleti (Set1 veya Tab10 gibi belirgin paletler)
    # PRB 0-9 arası için sabit renkler, -1 için gri
    cmap = plt.get_cmap('Set1')

    for node in data['nodes']:
        n_id = node['id']
        G.add_node(n_id)
        pos[n_id] = (node['x'], node['y'])
        prb = node['color']
        prb_map[n_id] = prb

        if prb == -1:
            node_colors.append('#95a5a6') # Gri (Conflict/Fail)
        else:
            node_colors.append(cmap(prb % 9)) # Renk döngüsü

    for edge in data['edges']:
        G.add_edge(edge['source'], edge['target'])


    # Kapsama Alanlarını Çiz
    # Her istasyonun etrafına yarı saydam daireler ekleyelim
    # Radius'u ortalama mesafeye göre tahmini bir değer veriyoruz (Görsel amaçlı)
    # Sınır çizgisi (Border)
    for n_id, (x, y) in pos.items():
        circle = Circle((x, y), radius=15, color=node_colors[n_id], alpha=0.1, linewidth=0)
        ax.add_patch(circle)
        circle_border = Circle((x, y), radius=15, color=node_colors[n_id], fill=False, alpha=0.3, linestyle='--')
        ax.add_patch(circle_border)


    # Kenarları (Girişimleri) Çiz
    # Mesafeye göre otomatik alpha (şeffaflık) ayarı yapmak mümkün ama basit tutalım
    nx.draw_networkx_edges(G, pos, ax=ax, width=1.2, alpha=0.3, edge_color='#7f8c8d')

    #Düğümleri (Baz İstasyonlarını) Çiz
    nx.draw_networkx_nodes(G, pos, ax=ax,
                           node_color=node_colors,
                           node_size=600,
                           edgecolors='white',
                           linewidths=2)

    # Etiketleri Yaz
    # Okunabilirlik için siyah outline (gölge) ekleyelim
    nx.draw_networkx_labels(G, pos, ax=ax, font_size=10, font_weight='bold', font_color='white')

    import matplotlib.patheffects as path_effects
    texts = nx.draw_networkx_labels(G, pos, ax=ax, font_size=10, font_weight='bold', font_color='white')
    for _, text in texts.items():
        text.set_path_effects([path_effects.withStroke(linewidth=3, foreground='black')])


    # İstatistik Kutusu
    total = len(data['nodes'])
    success = sum(1 for n in data['nodes'] if n['color'] != -1)
    fail = total - success
    success_rate = (success / total) * 100
    used_prbs = len(set(n['color'] for n in data['nodes'] if n['color'] != -1))

    stats_text = (
        f" NETWORK PERFORMANCE REPORT\n"
        f"──────────────────────────\n"
        f"Nodes (Base Stations) : {total}\n"
        f"Successful Allocation : {success}\n"
        f"Conflicts / Failed    : {fail}\n"
        f"Success Rate          : {success_rate:.1f}%\n"
        f"Unique PRBs Used      : {used_prbs}\n"
        f"──────────────────────────\n"
        f"Algorithm : Distributed Game Theory\n"
        f"Model     : Path Loss + Shadowing"
    )


    props = dict(boxstyle='round,pad=1', facecolor='white', alpha=0.9, edgecolor='gray')
    ax.text(0.99, 0.99, stats_text, transform=ax.transAxes, fontsize=9,
            verticalalignment='top', horizontalalignment='right', bbox=props, fontfamily='monospace')

    #Lejant (Frekans Renkleri)
    # Mevcut PRB'leri bul ve sırala
    legend_handles = []
    active_prbs = sorted(list(set(prb_map.values())))
    for prb in active_prbs:
        color = '#95a5a6' if prb == -1 else cmap(prb % 9)
        label = "No Service" if prb == -1 else f"Frequency Block {prb}"
        legend_handles.append(mlines.Line2D([], [], color='white', marker='o', markersize=12,
                                            markerfacecolor=color, label=label, markeredgecolor='gray'))

    ax.legend(handles=legend_handles, loc='lower left', title="Resource Blocks (PRB)", frameon=True, framealpha=0.8)

    # Başlık ve Ayarlar
    plt.title("5G/6G Heterogeneous Network Simulation\nDistributed Resource Allocation via Game Theory",
              fontsize=16, fontweight='bold', pad=12 ,y=0.94)
    plt.axis('off') # Eksenleri kapat

    # Dosyayı Yüksek Çözünürlükte Kaydet
    plt.tight_layout()
    plt.savefig("network_simulation_result.png", dpi=300, bbox_inches='tight')
    print("The graphic has been saved in high quality as “network_simulation_result.png”")
    plt.show()

if __name__ == "__main__":
    main()
