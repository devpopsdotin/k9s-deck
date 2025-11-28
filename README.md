
# K9s Deck (v1.0.0)

**K9s Deck** is a high-performance, cross-platform plugin for [K9s](https://k9scli.io/) written in **Go**. It transforms the standard Deployment view into a powerful dashboard, allowing engineers to visualize the relationship between Deployments, Pods, Helm Releases, Secrets, and ConfigMaps in real-time.

Built with the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework.

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8.svg)

---

## 🌟 Key Features

*   **Real-Time Monitoring:** Auto-refreshes resource status every second.
*   **Multi-Deployment Support:** Monitor multiple deployments simultaneously with stable, flicker-free UI.
*   **Smart Status Detection:** Accurately distinguishes between `Running`, `ContainerCreating`, and `Terminating` states, handling complex edge cases where Kubernetes reports "Waiting" for fully Ready pods.
*   **Split-Screen UI:** Browse resources on the left (35% width), view live details (YAML/Logs/Events) on the right.
*   **Quick Action Shortcuts:** Lightning-fast operations with `rr` (restart), `s` (scale), `R` (rollback), `+` (add), `rm` (remove).
*   **Command Mode (`:`):** Vim-style command bar to Scale, Restart, Rollback, Add, and Remove deployments directly from the plugin.
*   **Tabbed Interface:** Toggle between Configuration (YAML) and Live Data (Logs/Events) with a single key.
*   **Robust & Fast:** Includes strict timeouts (2s) on API calls to prevent UI freezing and "Smart Truncation" to handle long resource names on smaller screens.
*   **Manual Control:** Force refresh data (`Ctrl+F`) when the API server is slow to propagate changes.
*   **Quick Navigation:** Jump to specific resource types instantly using number keys (1-5). Supports cycling through multiple resources of the same type.

---

## 🛠️ Installation

### 1. Download Binary
1.  Go to the [Releases Page](https://github.com/devpopsdotin/k9s-deck/releases) on GitHub.
2.  Download the binary for your OS (Windows, macOS, or Linux).
3.  Rename the file to `k9s-deck` (or `k9s-deck.exe` on Windows).
4.  Move it to a permanent location (e.g., `/usr/local/bin/` or `~/.k9s/plugins/`).

### 2. Configure K9s
You need to register the plugin in your K9s configuration.

1.  Locate your K9s plugins file:
    *   **macOS:** `~/Library/Application Support/k9s/plugins.yaml`
    *   **Linux:** `~/.config/k9s/plugins.yaml`
    *   **Windows:** `%LOCALAPPDATA%\k9s\plugins.yaml`

2.  Add the configuration below. A ready-to-use **`plugins.yaml`** file is also included in this repository.

```yaml
plugins:
  k9s-deck:
    shortCut: Shift-I
    description: "K9s Deck"
    scopes:
      - deployments
    # ⚠️ UPDATE THIS PATH to where you saved the binary
    command: "/usr/local/bin/k9s-deck"
    background: false
    args:
      - $CONTEXT
      - $NAMESPACE
      - $NAME
```

### Option B: Build from Source
If you prefer to compile it yourself:

1.  **Clone & Init:**
    ```bash
    mkdir k9s-deck
    cd k9s-deck
    # Copy main.go here
    go mod init k9s-deck
    go mod tidy
    ```

2.  **Build:**
    ```bash
    go build -o k9s-deck main.go
    ```

---

## 📖 User Manual

### Navigation & Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **↑ / ↓** | Global | Select a resource (Pod, Secret, Helm, etc.). |
| **1 - 5** | Global | **Quick Jump**: 1=Dep, 2=Helm, 3=CM, 4=Secret, 5=Pod.<br>*(Press repeatedly to cycle through items)* |
| **Tab** | DEP / POD | **Toggle View**: Switch between YAML <-> Events (Deployment) or YAML <-> Logs (Pod). |
| **Enter** | Global | Refresh the details pane for the selected item. |
| **Ctrl + F** | Global | **Force Refresh**: Manually trigger a data fetch if the UI seems stale. |
| **Ctrl + L** | Pod | **Quick Logs**: View the last 200 lines of logs in the right pane. |
| **Ctrl + S** | Pod | **Search Logs**: Opens full logs in `less` for searching (`/pattern`). |
| **:** | Global | Enter **Command Mode**. |
| **/** | Global | Enter **Filter Mode**. |
| **q** | Global | Quit the plugin. |

### ⚡ Quick Action Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **rr** | Global | **Restart Deployment**: Double-tap 'r' to instantly restart the current deployment. |
| **s** | Global | **Scale Deployment**: Opens prompt to enter replica count. |
| **R** | Global | **Rollback Deployment**: Opens prompt to enter revision number (requires Helm release). |
| **+** | Global | **Add Deployment**: Opens prompt to add another deployment to monitor. |
| **rm** | Global | **Remove Deployment**: Press 'r' then 'm' to remove deployment from monitoring. |

### Command Mode (`:`)

Press `:` to focus the command bar at the bottom. Type your command and press Enter.

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **Scale** | `:scale <N>` | Scales the deployment to `N` replicas (e.g., `:scale 3`). |
| **Restart** | `:restart` | Triggers a rolling restart (`kubectl rollout restart`). |
| **Rollback** | `:rollback <Rev>` | Rolls back the Helm release to a specific revision (e.g., `:rollback 5`). |
| **Add** | `:add <name>` | Adds another deployment to monitor (e.g., `:add web-frontend`). |
| **Remove** | `:remove <name>` | Removes a deployment from monitoring (e.g., `:remove web-frontend`). |
| **Fetch** | `:fetch` | Alias for Force Refresh. |

---

## 🧠 Architecture & Logic

### Smart Status Detection
Standard `kubectl` JSON sometimes reports a Pod as "Waiting" (reason: `ContainerCreating`) even after it is `Running` and `Ready`. K9s Deck v1.0.0 fixes this:
1.  It calculates `Ready / Total` containers.
2.  If `Ready == Total`, it forces the status to **Running (1/1)**, ignoring historical waiting reasons.
3.  It only reports errors (like `CrashLoopBackOff`) if the pod is **not** ready.

### Resource Map
The Deck automatically discovers and links:
*   🚀 **Deployment:** The root object.
*   ⚓ **Helm Release:** detected via `meta.helm.sh/release-name` annotation or label.
*   📦 **Pods:** Live pods controlled by the deployment.
*   🔒 **Secrets:** Referenced in `envFrom`, `valueFrom`, or `volumes`.
*   📜 **ConfigMaps:** Referenced in `envFrom`, `valueFrom`, or `volumes`.

---

## 🐛 Troubleshooting

**1. Pods stuck in "Terminating"**
If the Kubernetes API is slow, the plugin might miss the deletion event.
*   **Fix:** Press `Ctrl+F` (Force Refresh).

**2. UI Freezes**
Calls to `kubectl` are wrapped in a 2-second timeout. If your cluster is unresponsive, you will see an error message in the header (e.g., `Err: context deadline exceeded`).

**3. "Unknown Command" in text input**
Ensure you are typing the command exactly as listed (e.g., `scale 1`, not `scale=1`).
