# K9s Deployment Lens (v3.5)

**K9s Deployment Lens** is a high-performance, cross-platform plugin for [K9s](https://k9scli.io/) written in **Go**. It transforms the standard Deployment view into a powerful dashboard, allowing engineers to visualize the relationship between Deployments, Pods, Helm Releases, Secrets, and ConfigMaps in real-time.

Built with the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework.

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8.svg)

---

## 🌟 Key Features

*   **Real-Time Monitoring:** Auto-refreshes resource status every second.
*   **Smart Status Detection:** Accurately distinguishes between `Running`, `ContainerCreating`, and `Terminating` states, handling complex edge cases where Kubernetes reports "Waiting" for fully Ready pods.
*   **Split-Screen UI:** Browse resources on the left (40% width), view live details (YAML/Logs/Events) on the right.
*   **Command Mode (`:`):** Vim-style command bar to Scale, Restart, and Rollback directly from the plugin.
*   **Tabbed Interface:** Toggle between Configuration (YAML) and Live Data (Logs/Events) with a single key.
*   **Robust & Fast:** Includes strict timeouts (2s) on API calls to prevent UI freezing and "Smart Truncation" to handle long resource names on smaller screens.
*   **Manual Control:** Force refresh data (`Ctrl+F`) when the API server is slow to propagate changes.

---

## 🛠️ Installation

### Prerequisites
*   **Go 1.21+** installed.
*   **Kubernetes CLI** (`kubectl`) installed and configured.
*   **Helm CLI** (`helm`) installed.

### 1. Initialize & Install Dependencies
Create a directory for the plugin and install the required Go modules:

```bash
mkdir k9s-lens
cd k9s-lens
go mod init k9s-lens

go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles/viewport
go get github.com/charmbracelet/bubbles/textinput
go get github.com/tidwall/gjson
go get github.com/alecthomas/chroma/v2
go get github.com/alecthomas/chroma/v2/quick
go get github.com/alecthomas/chroma/v2/styles
go mod tidy
```

### 2. Build the Binary
Copy the source code (from `main.go`) into the folder and build:

```bash
# macOS / Linux
go build -o k9s-lens main.go

# Windows
go build -o k9s-lens.exe main.go
```

### 3. Configure K9s
Move the binary to a location of your choice (e.g., `~/.k9s/plugins/`) and update your K9s `plugins.yaml` file:

```yaml
# location: $HOME/Library/Application Support/k9s/plugins.yaml (macOS)
# location: $HOME/.config/k9s/plugins.yaml (Linux)

plugins:
  go-lens:
    shortCut: Shift-I
    description: "Deployment Lens"
    scopes:
      - deployments
    command: "/path/to/your/k9s-deck" # <--- UPDATE THIS PATH
    background: false
    args:
      - $CONTEXT
      - $NAMESPACE
      - $NAME
```

---

## 📖 User Manual

### Navigation & Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **↑ / ↓** | Global | Select a resource (Pod, Secret, Helm, etc.). |
| **Tab** | DEP / POD | **Toggle View**: Switch between YAML <-> Events (Deployment) or YAML <-> Logs (Pod). |
| **Enter** | Global | Refresh the details pane for the selected item. |
| **Ctrl + F** | Global | **Force Refresh**: Manually trigger a data fetch if the UI seems stale. |
| **Ctrl + L** | Pod | **Quick Logs**: View the last 200 lines of logs in the right pane. |
| **Ctrl + S** | Pod | **Search Logs**: Opens full logs in `less` for searching (`/pattern`). |
| **:** | Global | Enter **Command Mode**. |
| **q** | Global | Quit the plugin. |

### Command Mode (`:`)

Press `:` to focus the command bar at the bottom. Type your command and press Enter.

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **Scale** | `:scale <N>` | Scales the deployment to `N` replicas (e.g., `:scale 3`). |
| **Restart** | `:restart` | Triggers a rolling restart (`kubectl rollout restart`). |
| **Rollback** | `:rollback <Rev>` | Rolls back the Helm release to a specific revision (e.g., `:rollback 5`). |
| **Fetch** | `:fetch` | Alias for Force Refresh. |

---

## 🧠 Architecture & Logic

### Smart Status Detection
Standard `kubectl` JSON sometimes reports a Pod as "Waiting" (reason: `ContainerCreating`) even after it is `Running` and `Ready`. K9s Lens v3.5 fixes this:
1.  It calculates `Ready / Total` containers.
2.  If `Ready == Total`, it forces the status to **Running (1/1)**, ignoring historical waiting reasons.
3.  It only reports errors (like `CrashLoopBackOff`) if the pod is **not** ready.

### Resource Map
The Lens automatically discovers and links:
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
