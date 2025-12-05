import json
import matplotlib.pyplot as plt
import networkx as nx
import matplotlib.cm as cm

def main():
    filename = 'viz_data.json'
    try:
        with open(filename, 'r') as f:
            data = json.load(f)
        print(f" '{filename}' successfully loaded.")
    except FileNotFoundError:
        print(f" ERROR: '{filename}' not found.")
        print("   Please run the Go simulation first.")
        return

    G = nx.Graph()

    pos = {}
    node_prbs = []
    labels = {}
    ordered_nodes = []
    for node in data['nodes']:
        n_id = node['id']

        ordered_nodes.append(n_id)
        G.add_node(n_id)

        pos[n_id] = (node['x'], node['y'])
        node_prbs.append(node['color'])

        labels[n_id] = f"{n_id}"

    for edge in data['edges']:
        G.add_edge(edge['source'], edge['target'])

    cmap = plt.get_cmap('tab20')

    node_colors = []
    for prb in node_prbs:
        if prb == -1:
            node_colors.append('lightgray')
        else:
            node_colors.append(cmap(prb % 20))

    # 6. Drawing Process
    plt.figure(figsize=(12, 10))

    nx.draw_networkx_edges(G, pos,
                           width=1.5,
                           alpha=0.4,
                           edge_color='gray')

    nx.draw_networkx_nodes(G, pos,
                           nodelist=ordered_nodes,
                           node_color=node_colors,
                           node_size=700,
                           edgecolors='black', # Black frame
                           linewidths=1.0)

    nx.draw_networkx_labels(G, pos,
                            labels=labels,
                            font_size=9,
                            font_weight='bold',
                            font_color='black')

    plt.title(f"5G Distributed Resource Management - {len(ordered_nodes)} Base Stations", fontsize=15)
    plt.axis('off') # Hide axis lines

    plt.text(0.05, 0.95, "Colors represent different PRB (Frequency) blocks.\nGray: Assignment Failed",
             transform=plt.gca().transAxes, fontsize=10,
             verticalalignment='top', bbox=dict(boxstyle='round', facecolor='wheat', alpha=0.5))

    total_stations = len(ordered_nodes)
    successful_stations = sum(1 for n in node_prbs if n != -1)
    failed_stations = total_stations - successful_stations
    unique_prbs = len(set(n for n in node_prbs if n != -1))


    print("-------------- SIMULATION ANALYSIS REPORT ---------------------------")
    print(f"🔹 Total Base Stations : {total_stations}")
    print("------------------------------------------------------------------")
    print(f" Successful Assignments : {successful_stations}")
    print("------------------------------------------------------------------")
    print(f" Failed (Conflicts)     : {failed_stations}")
    print("------------------------------------------------------------------")
    print(f" Success Rate           : %{100 * successful_stations / total_stations:.1f}")
    print("------------------------------------------------------------------")
    print(f" Number of Used PRBs    : {unique_prbs}")
    print("-" * 40)

    # 8. Show
    plt.show()

if __name__ == "__main__":
    main()
