import matplotlib.pyplot as plt
import numpy as np
from matplotlib.patches import FancyBboxPatch, Circle
import matplotlib.patches as mpatches

# Set style
plt.style.use('seaborn-v0_8-darkgrid')
plt.rcParams['font.family'] = 'sans-serif'
plt.rcParams['font.sans-serif'] = ['Arial', 'DejaVu Sans']

# --- SIMULATION DATA ---
distributed_cost = 0.0000
centralized_cost = 0.0000
poa_value = 1.0000

# Network metrics
total_capacity = 9839.08  # Mbps
avg_speed = 245.98  # Mbps
fairness = 0.8172
num_bs = 40

# --- CREATE FIGURE ---
fig = plt.figure(figsize=(14, 10))
gs = fig.add_gridspec(3, 2, height_ratios=[2, 1, 0.3], hspace=0.3, wspace=0.3)

# --- MAIN PLOT: PoA Comparison ---
ax_main = fig.add_subplot(gs[0, :])

# Create bars with gradient effect
categories = ['Centralized\nGreedy', 'Distributed\nGame Theory']
values = [0.001, 0.001]  # Visual epsilon
colors = ['#34495e', '#27ae60']
edge_colors = ['#2c3e50', '#229954']

bars = ax_main.bar(categories, values, width=0.4, color=colors,
                   edgecolor=edge_colors, linewidth=3, alpha=0.9)

# Add glow effect
for bar, color in zip(bars, colors):
    bar.set_zorder(2)
    # Shadow effect
    shadow = ax_main.bar(bar.get_x(), bar.get_height() * 1.05,
                         bar.get_width(), color=color, alpha=0.2, zorder=1)

ax_main.set_ylim(0, 0.005)
ax_main.set_ylabel('Global Interference Cost', fontsize=14, fontweight='bold')
ax_main.set_title('Price of Anarchy: Algorithm Efficiency Comparison',
                  fontsize=18, fontweight='bold', pad=20)
ax_main.set_yticks([0])
ax_main.set_yticklabels(['0.0000\n(Zero Interference)'], fontsize=11)
ax_main.grid(True, alpha=0.3, linestyle='--', axis='y')
ax_main.set_axisbelow(True)

# Add value labels on bars
for i, (bar, val) in enumerate(zip(bars, [distributed_cost, centralized_cost])):
    height = bar.get_height()
    ax_main.text(bar.get_x() + bar.get_width()/2, height + 0.0003,
                 f'{val:.4f}',
                 ha='center', va='bottom', fontsize=16, fontweight='bold',
                 color=edge_colors[i])

    # Add checkmark
    ax_main.text(bar.get_x() + bar.get_width()/2, height/2,
                 '✓', ha='center', va='center', fontsize=40,
                 color='white', fontweight='bold', alpha=0.7)

# --- PoA BADGE ---
badge_x, badge_y = 0.5, 0.003
badge = FancyBboxPatch((badge_x - 0.35, badge_y - 0.0003), 0.7, 0.0018,
                       boxstyle="round,pad=0.02",
                       facecolor='#27ae60', edgecolor='#229954',
                       linewidth=3, alpha=0.3, transform=ax_main.transData,
                       zorder=3)
ax_main.add_patch(badge)

ax_main.text(badge_x, badge_y + 0.0005,
             f'Price of Anarchy (PoA) = {poa_value:.4f}',
             ha='center', va='center', fontsize=15, fontweight='bold',
             bbox=dict(boxstyle='round,pad=0.5', facecolor='#27ae60',
                       edgecolor='#229954', linewidth=2, alpha=0.9),
             color='white', zorder=4)

ax_main.text(badge_x, badge_y - 0.0005,
             '🏆 Perfect Nash Equilibrium Achieved',
             ha='center', va='center', fontsize=12, fontweight='bold',
             color='#27ae60', style='italic')

# --- METRIC CARDS ---
# Card 1: Network Capacity
ax_card1 = fig.add_subplot(gs[1, 0])
ax_card1.axis('off')

card1_patch = FancyBboxPatch((0.05, 0.1), 0.9, 0.8,
                             boxstyle="round,pad=0.05",
                             facecolor='#3498db', edgecolor='#2980b9',
                             linewidth=2, alpha=0.2)
ax_card1.add_patch(card1_patch)

ax_card1.text(0.5, 0.75, '📊 Network Performance', ha='center', va='center',
              fontsize=13, fontweight='bold', color='#2c3e50')
ax_card1.text(0.5, 0.55, f'{total_capacity:.2f} Mbps', ha='center', va='center',
              fontsize=20, fontweight='bold', color='#2980b9')
ax_card1.text(0.5, 0.35, 'Total Capacity', ha='center', va='center',
              fontsize=10, color='#34495e')
ax_card1.text(0.5, 0.2, f'Avg: {avg_speed:.2f} Mbps/BS', ha='center', va='center',
              fontsize=10, color='#7f8c8d', style='italic')

# Card 2: System Metrics
ax_card2 = fig.add_subplot(gs[1, 1])
ax_card2.axis('off')

card2_patch = FancyBboxPatch((0.05, 0.1), 0.9, 0.8,
                             boxstyle="round,pad=0.05",
                             facecolor='#e67e22', edgecolor='#d35400',
                             linewidth=2, alpha=0.2)
ax_card2.add_patch(card2_patch)

ax_card2.text(0.5, 0.75, '⚖️ System Quality', ha='center', va='center',
              fontsize=13, fontweight='bold', color='#2c3e50')
ax_card2.text(0.5, 0.55, f'{fairness:.4f}', ha='center', va='center',
              fontsize=20, fontweight='bold', color='#d35400')
ax_card2.text(0.5, 0.35, "Jain's Fairness Index", ha='center', va='center',
              fontsize=10, color='#34495e')
ax_card2.text(0.5, 0.2, f'{num_bs} Base Stations', ha='center', va='center',
              fontsize=10, color='#7f8c8d', style='italic')

# --- BOTTOM NOTE ---
ax_note = fig.add_subplot(gs[2, :])
ax_note.axis('off')

note_text = (
    "✓ Both algorithms eliminated interference completely (0.0000)\n"
    "✓ PoA = 1.0 confirms distributed system matches centralized optimum\n"
    "✓ Nash Equilibrium achieved without central coordination"
)

ax_note.text(0.5, 0.5, note_text,
             ha='center', va='center', fontsize=10,
             bbox=dict(boxstyle='round,pad=0.8', facecolor='#ecf0f1',
                       edgecolor='#bdc3c7', linewidth=1.5),
             color='#2c3e50', linespacing=1.8)

# Add algorithm comparison legend
legend_elements = [
    mpatches.Patch(facecolor='#34495e', edgecolor='#2c3e50',
                   label='Centralized Greedy (Benchmark)', linewidth=2),
    mpatches.Patch(facecolor='#27ae60', edgecolor='#229954',
                   label='Distributed Game Theory (Proposed)', linewidth=2)
]
ax_main.legend(handles=legend_elements, loc='upper right',
               fontsize=11, framealpha=0.95, edgecolor='#95a5a6',
               fancybox=True, shadow=True)

plt.tight_layout()
plt.savefig('poa_analysis_premium.png', dpi=300, bbox_inches='tight',
            facecolor='white', edgecolor='none')
plt.show()

print(" Premium graph saved as 'poa_analysis_premium.png'")
print(" Features: Modern design, metric cards, enhanced visuals")