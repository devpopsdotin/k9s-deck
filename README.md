
# K9s Deck (v2.0.0)

**K9s Deck** is a high-performance, cross-platform plugin for [K9s](https://k9scli.io/) written in **Go**. It transforms the standard Deployment view into a powerful dashboard, allowing engineers to visualize the relationship between Deployments, Pods, Helm Releases, Secrets, and ConfigMaps in real-time.

**v2.0.0** features a complete architectural refactoring with modular packages, comprehensive testing (32 unit tests), and thread-safe concurrent operations.

Built with the [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI framework.

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8.svg)

---

## 🌟 Key Features

*   **Real-Time Monitoring:** Auto-refreshes resource status every second.
*   **Multi-Deployment Support:** Monitor multiple deployments simultaneously with stable, flicker-free UI.
*   **Smart Status Detection:** Accurately distinguishes between `Running`, `ContainerCreating`, and `Terminating` states, handling complex edge cases where Kubernetes reports "Waiting" for fully Ready pods.
*   **Enhanced Log Formatting:** Color-coded log levels (ERROR/WARN/INFO), smart pod prefixes with colored icons, automatic JSON pretty-printing with syntax highlighting, and toggle between raw/formatted views.
*   **Split-Screen UI:** Browse resources on the left (35% width), view live details (YAML/Logs/Events) on the right.
*   **Keyboard Viewport Scrolling:** Full vim-style keyboard navigation for scrolling through logs and details (Ctrl+d/u for half-page, Ctrl+e/y for line-by-line, Page Up/Down).
*   **Quick Action Shortcuts:** Lightning-fast operations with `rr` (restart), `s` (scale), `R` (rollback), `+` (add), `-` (remove).
*   **LSP-like Autocomplete:** Intelligent deployment suggestions with real-time filtering for add/remove operations.
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
| **↑ / ↓** or **j / k** | Global | Select a resource (Pod, Secret, Helm, etc.). |
| **1 - 5** | Global | **Quick Jump**: 1=Dep, 2=Helm, 3=CM, 4=Secret, 5=Pod.<br>*(Press repeatedly to cycle through items)* |
| **Tab** | DEP / POD | **Toggle View**: Switch between YAML <-> Events (Deployment) or YAML <-> Logs (Pod). |
| **f** | Logs | **Toggle Format**: Switch between formatted (colored, enhanced) and raw log view. |
| **y** | Global | **Yank (Copy)**: Copy entire right pane content to clipboard (vim-style). |
| **Enter** | Global | Refresh the details pane for the selected item. |
| **Ctrl + F** | Global | **Force Refresh**: Manually trigger a data fetch if the UI seems stale. |
| **Ctrl + L** | Pod | **Quick Logs**: View the last 200 lines of logs in the right pane. |
| **Ctrl + S** | Pod | **Search Logs**: Opens full logs in `less` for searching (`/pattern`). |
| **:** | Global | Enter **Command Mode**. |
| **/** | Global | Enter **Filter Mode**. |
| **q** | Global | Quit the plugin. |

### Viewport Scrolling (Logs/Details Panel)

| Key | Action |
| :--- | :--- |
| **Ctrl + d** | Scroll down half page (vim-style) |
| **Ctrl + u** | Scroll up half page (vim-style) |
| **Ctrl + e** | Scroll down one line (vim-style) |
| **Ctrl + y** | Scroll up one line (vim-style) |
| **Page Down** | Scroll down one full page |
| **Page Up** | Scroll up one full page |

### ⚡ Quick Action Shortcuts

| Key | Context | Action |
| :--- | :--- | :--- |
| **rr** | Global | **Restart Deployment**: Double-tap 'r' to instantly restart the current deployment. |
| **s** | Global | **Scale Deployment**: Opens prompt to enter replica count. |
| **R** | Global | **Rollback Deployment**: Opens prompt to enter revision number (requires Helm release). |
| **+** | Global | **Add Deployment**: Opens LSP-like autocomplete with available cluster deployments (excludes monitored ones). |
| **-** | Global | **Remove Deployment**: Opens LSP-like autocomplete with currently monitored deployments to remove. |

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

## 🔍 LSP-like Autocomplete

K9s Deck includes intelligent autocomplete functionality for deployment management:

### Add Deployment (`+`)
- **Smart Filtering**: Shows only deployments available in the cluster that aren't already being monitored
- **Real-time Search**: Type to filter deployments by name (case-insensitive)
- **Keyboard Navigation**: Use ↑↓ arrows to navigate through suggestions
- **Tab Completion**: Press Tab to auto-complete with the selected deployment
- **Visual Feedback**: Selected suggestion is highlighted with ▶ and colored text

### Remove Deployment (`-`)
- **Context-Aware**: Shows only deployments currently being monitored
- **Same UX**: Identical keyboard navigation and completion as add mode
- **Safety**: Can't remove deployments that aren't being monitored

### Navigation Keys
| Key | Action |
| :--- | :--- |
| **Type** | Filter suggestions by name |
| **↑ / ↓** | Navigate through suggestions |
| **Tab** | Complete with selected suggestion |
| **Enter** | Add/Remove the selected or typed deployment |
| **Esc** | Cancel and return to normal mode |

---

## 🎨 Enhanced Log Formatting

K9s Deck includes powerful log formatting capabilities to improve readability and help identify issues quickly:

### Color-Coded Log Levels
Log levels are automatically detected and color-coded for quick visual scanning:
- **ERROR / FATAL**: Red highlighting
- **WARN / WARNING**: Yellow highlighting
- **INFO**: Cyan highlighting
- **DEBUG / TRACE**: Gray highlighting

The detection is case-insensitive and works with various log formats (structured, plain text, JSON).

### Smart Pod Prefixes
When viewing deployment logs with multiple pods:
- **Shortened Prefixes**: Pod names are intelligently shortened to `[..abc123/container]` format, keeping the unique 7-character suffix
- **Colored Icons**: Each pod gets a consistent color with a `●` icon for easy visual distinction
- **Hash-Based Colors**: Same pod always gets the same color across sessions using a 10-color palette

### Multi-Container Detection
For pod logs:
- **Automatic Detection**: System detects if a pod has multiple containers
- **Smart Prefix**: Prefixes are only shown for multi-container pods
- **Cached**: Container count is cached to avoid repeated kubectl calls

### JSON Pretty-Printing
JSON log lines are automatically detected and enhanced:
- **Auto-Detection**: Identifies JSON by bracket matching
- **Pretty-Printing**: Formats with 2-space indentation
- **Syntax Highlighting**: Full Chroma-powered syntax coloring for JSON structure
- **Graceful Fallback**: Invalid JSON is displayed as-is

### Format Toggle
Press **`f`** to switch between:
- **Formatted Mode** (default): All enhancements active - colors, smart prefixes, JSON formatting
- **Raw Mode**: Original kubectl output unchanged, useful for copying or debugging

The current mode is displayed in the footer: `(Formatted)` or `(Raw)`

---

## 🧠 Architecture & Logic

### Smart Status Detection
Standard `kubectl` JSON sometimes reports a Pod as "Waiting" (reason: `ContainerCreating`) even after it is `Running` and `Ready`. K9s Deck fixes this:
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

---

## 🔧 Development

### Architecture (v2.0.0+)

K9s Deck uses a modular architecture with clear separation of concerns:

```
k9s-deck/
├── main.go                        # Entry point & Bubble Tea UI
├── internal/
│   ├── logger/                    # Structured logging (slog)
│   ├── parser/                    # Log parsing & syntax highlighting
│   ├── k8s/                       # Kubernetes operations (kubectl/helm)
│   ├── state/                     # Thread-safe state management
│   └── ui/                        # UI components & styles
└── testdata/                      # Test fixtures
```

### Building from Source

```bash
# Clone the repository
git clone https://github.com/devpopsdotin/k9s-deck.git
cd k9s-deck

# Build
go build .

# Run tests
go test ./...

# Run tests with race detector
go test -race ./...
```

### Testing

Comprehensive test suite with 32 unit tests:

```bash
go test ./...                    # Run all tests
go test -v ./internal/parser     # Parser tests (7 tests)
go test -v ./internal/k8s        # K8s tests (14 tests)
go test -v ./internal/state      # State tests (11 tests, race-free)
go test -race ./...              # Run with race detector
```

### Logging

Application logs are written to `/tmp/k9s-deck.log` in structured JSON format:

```bash
tail -f /tmp/k9s-deck.log        # Monitor logs
```

Set log level via environment variable:
```bash
export K9S_DECK_LOG_LEVEL=DEBUG  # DEBUG, INFO, WARN, ERROR
```

### Key Improvements in v2.0.0

- ✅ **Modular architecture** - 6 organized packages
- ✅ **Thread-safe** - Fixed race condition with StateManager
- ✅ **Tested** - 32 unit tests, 0 races detected
- ✅ **Observable** - Structured logging with slog
- ✅ **Maintainable** - Clear separation of concerns
