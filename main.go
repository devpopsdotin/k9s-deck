package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tidwall/gjson"

	"github.com/devpopsdotin/k9s-deck/internal/k8s"
	"github.com/devpopsdotin/k9s-deck/internal/logger"
)

// --- CONFIG ---
var (
	Context    string
	Namespace  string
	Deployment string
	client     k8s.Client // Kubernetes client (client-go)
)

// --- CONSTANTS ---
const (
	// Timing
	RefreshInterval    = 1 * time.Second
	CommandTimeout     = 2 * time.Second
	LongCommandTimeout = 5 * time.Second
	TickerInterval     = 1 * time.Second

	// UI Layout
	LeftPaneWidthRatio = 0.35
	MinLeftPaneWidth   = 20
	MinWrapWidth       = 10
	HeaderHeight       = 3
	FooterHeight       = 1
	UILayoutPadding    = 2

	// Logging
	DefaultLogTailLines = 200
	DeploymentLogTail   = 100

	// Log Formatting
	PodPrefixSuffixLen  = 7
	MaxPodPrefixDisplay = 20
	JSONIndent          = 2

	// List Display
	DefaultListHeight = 20
	MaxSuggestions    = 5

	// Validation
	MaxK8sNameLength = 253

	// Tabs
	DeploymentTabCount = 3
	PodTabCount        = 2
)

// --- STYLES ---
var (
	cPrimary   = lipgloss.Color("62")  // Purple/Blue
	cSecondary = lipgloss.Color("39")  // Cyan
	cGreen     = lipgloss.Color("42")  // Green
	cRed       = lipgloss.Color("196") // Red
	cYellow    = lipgloss.Color("220") // Yellow
	cGray      = lipgloss.Color("240") // Gray

	// Pod color palette for log prefixes
	podColorPalette = []lipgloss.Color{
		lipgloss.Color("39"),  // Cyan
		lipgloss.Color("42"),  // Green
		lipgloss.Color("220"), // Yellow
		lipgloss.Color("201"), // Magenta
		lipgloss.Color("141"), // Purple
		lipgloss.Color("208"), // Orange
		lipgloss.Color("51"),  // Light Blue
		lipgloss.Color("82"),  // Light Green
		lipgloss.Color("213"), // Pink
		lipgloss.Color("228"), // Light Yellow
	}

	styleBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(cGray)
	stylePane     = lipgloss.NewStyle().Padding(0, 1)
	styleTitle    = lipgloss.NewStyle().Foreground(cSecondary).Bold(true)
	styleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(cPrimary).Bold(true).Padding(0, 1)
	styleDim      = lipgloss.NewStyle().Foreground(cGray)
	styleErr      = lipgloss.NewStyle().Foreground(cRed)
	styleHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true).Background(lipgloss.Color("237")).Padding(0, 1).Width(100)

	styleTabActive   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(cPrimary).Foreground(cPrimary).Bold(true).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Padding(0, 1).Foreground(cGray)

	styleCmdBar = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("236")).Padding(0, 1)

	styleHighlight = lipgloss.NewStyle().Background(lipgloss.Color("201")).Foreground(lipgloss.Color("255")).Bold(true)
)

// --- LOG PARSING ---
var (
	logLevelRegex  = regexp.MustCompile(`(?i)\b(FATAL|ERROR|ERR|WARN|WARNING|INFO|DEBUG|TRACE)\b`)
	podPrefixRegex = regexp.MustCompile(`^\[([^/]+)/([^/]+)/([^\]]+)\]\s*(.*)$`)
)

func init() {
	_ = styles.Get("dracula")
}

// --- DATA MODEL ---
type item struct {
	Type   string // DEP, POD, HELM, SEC, CM, HDR
	Name   string
	Status string
}

type logLineInfo struct {
	OriginalLine  string
	PodPrefix     string // e.g., "nginx-deployment-5c7588df-abc123/nginx"
	PodName       string
	ContainerName string
	LogContent    string
	LogLevel      string // ERROR, WARN, INFO, DEBUG, etc.
	IsJSON        bool
}

type multiContainerCache struct {
	mu    sync.RWMutex
	cache map[string]bool // podName -> hasMultipleContainers
}

type model struct {
	items []item

	targets      []string          // List of deployments to monitor
	selectors    map[string]string // Cache label selectors per deployment
	helmReleases map[string]string // Cache helm release names

	cursor     int
	listOffset int
	listHeight int

	activeTab    int
	textInput    textinput.Model
	inputMode    bool
	filterMode   bool
	shortcutMode string // "scale", "rollback", "add", "remove", or ""
	partialKey   string // for multi-character shortcuts like "rm"
	activeFilter string
	filterRegex  *regexp.Regexp

	// LSP-like autocomplete
	suggestions     []string // Available deployment names for autocomplete
	suggestionIndex int      // Currently selected suggestion
	showSuggestions bool     // Whether to show autocomplete suggestions

	viewport   viewport.Model
	rawContent string
	ready      bool
	width      int
	height     int
	lastUpd    time.Time
	err        error

	// Log formatting
	logFormatMode      bool                 // true=formatted, false=raw
	multiContainerInfo *multiContainerCache // cache for multi-container detection

	// Status messages
	statusMsg string // temporary status message (e.g., "Copied to clipboard")
}

// --- MESSAGES ---
type tickMsg time.Time
type dataMsg struct {
	items        []item
	selectors    map[string]string
	helmReleases map[string]string
	err          error
}
type detailsMsg struct {
	content string
	isYaml  bool
	err     error
}
type commandFinishedMsg struct{}
type addTargetMsg struct {
	name string
}
type removeTargetMsg struct {
	name string
}
type suggestionsMsg struct {
	deployments []string
}
type copyMsg struct {
	success bool
	err     error
}
type clearStatusMsg struct{}

// --- MAIN ---
func main() {
	if len(os.Args) < 4 {
		if os.Getenv("KUBECONFIG") != "" {
			Context = "kind-kind"
			Namespace = "default"
			Deployment = "hello-app"
		} else {
			fmt.Println("Usage: k9s-deck <context> <namespace> <deployment>")
			os.Exit(1)
		}
	} else {
		Context = os.Args[1]
		Namespace = os.Args[2]
		Deployment = os.Args[3]
	}

	// Initialize logger (writes to /tmp/k9s-deck.log)
	if err := logger.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
		// Continue anyway - logging is not critical
	}

	// Initialize Kubernetes client (uses client-go for performance)
	var err error
	client, err = k8s.NewClient(Context)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "scale 3 | restart | rollback 1 | add <name> | remove <name>"
	ti.Prompt = ": "
	ti.CharLimit = 156
	ti.Width = 50

	// Initialize targets with the starting deployment
	return model{
		textInput:     ti,
		inputMode:     false,
		listHeight:    DefaultListHeight,
		targets:       []string{Deployment},
		selectors:     make(map[string]string),
		helmReleases:  make(map[string]string),
		logFormatMode: true, // Default to formatted
		multiContainerInfo: &multiContainerCache{
			cache: make(map[string]bool),
		},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.targets, m.selectors), tickCmd(), textinput.Blink)
}

// copySelectorMap creates a copy of selectors map to avoid concurrent access issues
func copySelectorMap(selectors map[string]string) map[string]string {
	copied := make(map[string]string, len(selectors))
	for k, v := range selectors {
		copied[k] = v
	}
	return copied
}

// ensureCursorInBounds ensures cursor is within valid range of items
func ensureCursorInBounds(cursor, itemCount int) int {
	if itemCount == 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= itemCount {
		return itemCount - 1
	}
	return cursor
}

// maxInt returns the larger of two integers
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt returns the smaller of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- UPDATE ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// --- SYSTEM MESSAGES ---
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(fetchDataCmd(m.targets, m.selectors), tickCmd())

	case commandFinishedMsg:
		return m, fetchDataCmd(m.targets, m.selectors)

	case addTargetMsg:
		// Check duplicates
		exists := false
		for _, t := range m.targets {
			if t == msg.name {
				exists = true
				break
			}
		}
		if !exists {
			m.targets = append(m.targets, msg.name)
		}
		return m, fetchDataCmd(m.targets, m.selectors)

	case removeTargetMsg:
		// Remove target from list
		var newTargets []string
		for _, t := range m.targets {
			if t != msg.name {
				newTargets = append(newTargets, t)
			}
		}
		m.targets = newTargets
		// Also clean up the selectors and helm releases for removed target
		delete(m.selectors, msg.name)
		delete(m.helmReleases, msg.name)
		// Reset cursor if needed
		if len(m.targets) == 0 {
			m.cursor = 0
		}
		return m, fetchDataCmd(m.targets, m.selectors)

	case suggestionsMsg:
		// Update available deployment suggestions (only for add mode)
		if m.shortcutMode == "add" {
			// Filter out already monitored deployments immediately
			var filtered []string
			for _, deployment := range msg.deployments {
				alreadyMonitored := false
				for _, target := range m.targets {
					if target == deployment {
						alreadyMonitored = true
						break
					}
				}
				if !alreadyMonitored {
					filtered = append(filtered, deployment)
				}
			}
			m.suggestions = filtered
			m.updateSuggestions()
		}
		// For remove mode, suggestions are already populated with current targets
		return m, nil

	case copyMsg:
		// Handle clipboard copy result
		if msg.success {
			m.statusMsg = "Yanked to clipboard"
		} else {
			m.statusMsg = fmt.Sprintf("Copy failed: %v", msg.err)
		}
		// Clear status message after 2 seconds
		return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return clearStatusMsg{}
		})

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case tea.WindowSizeMsg:
		m.width = maxInt(msg.Width, 0)
		m.height = maxInt(msg.Height, 0)

		m.listHeight = maxInt(msg.Height-HeaderHeight-FooterHeight-UILayoutPadding, 1)

		paneWidth := maxInt(int(float64(msg.Width)*LeftPaneWidthRatio), 0)
		vpWidth := maxInt(msg.Width-paneWidth-4, 0)
		vpHeight := maxInt(msg.Height-HeaderHeight-FooterHeight-UILayoutPadding, 0)

		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.YPosition = HeaderHeight + 1
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
			m.updateViewportContent()
		}
		return m, nil

	case dataMsg:
		m.lastUpd = time.Now()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil

			// Remember current selection before updating items
			var currentSelection *item
			if len(m.items) > 0 && m.cursor < len(m.items) {
				currentSelection = &m.items[m.cursor]
			}

			m.items = msg.items
			// Merge maps
			for k, v := range msg.selectors {
				m.selectors[k] = v
			}
			for k, v := range msg.helmReleases {
				m.helmReleases[k] = v
			}

			// Try to restore cursor to the same item
			if currentSelection != nil && len(m.items) > 0 {
				newCursor := -1
				for i, item := range m.items {
					if item.Type == currentSelection.Type && item.Name == currentSelection.Name {
						newCursor = i
						break
					}
				}
				if newCursor != -1 {
					m.cursor = newCursor
				} else {
					// Item not found, validate bounds
					m.cursor = ensureCursorInBounds(m.cursor, len(m.items))
				}
			} else {
				// Validate cursor position for new or empty selections
				m.cursor = ensureCursorInBounds(m.cursor, len(m.items))
			}

			// Always refresh details - pass a copy of selectors to avoid race
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
			}
		}
		return m, tea.Batch(cmds...)

	case detailsMsg:
		if msg.err != nil {
			m.rawContent = fmt.Sprintf("Error: %v", msg.err)
		} else {
			if msg.isYaml {
				m.rawContent = highlight(msg.content, "yaml")
			} else {
				// Determine if this is log content
				currentItem := item{}
				if len(m.items) > 0 && m.cursor < len(m.items) {
					currentItem = m.items[m.cursor]
				}

				isLogContent := (currentItem.Type == "DEP" && m.activeTab == 2) ||
					(currentItem.Type == "POD" && m.activeTab == 1)

				if isLogContent {
					m.rawContent = processLogContent(msg.content, currentItem.Type,
						currentItem.Name, m.logFormatMode)
				} else {
					m.rawContent = msg.content
				}
			}
		}
		m.updateViewportContent()
		return m, nil
	}

	// --- INPUT MODE ---
	if m.inputMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "tab":
				// Tab completes with selected suggestion for add/remove mode
				if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.showSuggestions && len(m.suggestions) > 0 {
					selectedSuggestion := m.suggestions[m.suggestionIndex]
					m.textInput.SetValue(selectedSuggestion)
					m.showSuggestions = false
					return m, textinput.Blink
				}
			case "up":
				// Navigate up in suggestions for add/remove mode
				if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.showSuggestions && len(m.suggestions) > 0 {
					if m.suggestionIndex > 0 {
						m.suggestionIndex--
					} else {
						m.suggestionIndex = len(m.suggestions) - 1
					}
					return m, nil
				}
			case "down":
				// Navigate down in suggestions for add/remove mode
				if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.showSuggestions && len(m.suggestions) > 0 {
					if m.suggestionIndex < len(m.suggestions)-1 {
						m.suggestionIndex++
					} else {
						m.suggestionIndex = 0
					}
					return m, nil
				}
			case "enter":
				val := m.textInput.Value()
				m.inputMode = false
				m.textInput.Blur()

				if m.filterMode {
					m.activeFilter = val
					if val != "" {
						re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(val))
						if err == nil {
							m.filterRegex = re
						}
					} else {
						m.filterRegex = nil
					}
					m.filterMode = false
					m.updateViewportContent()
				} else if m.shortcutMode != "" {
					// Handle shortcut mode input
					m.textInput.Reset()
					shortcut := m.shortcutMode
					m.shortcutMode = ""

					switch shortcut {
					case "scale":
						// Validate scale value is a positive integer
						if val == "" {
							m.rawContent = "Scale value cannot be empty"
							m.updateViewportContent()
							return m, nil
						}
						// Simple validation - check if it's a number
						if strings.TrimSpace(val) == "" || !isPositiveInteger(val) {
							m.rawContent = "Scale value must be a positive integer"
							m.updateViewportContent()
							return m, nil
						}
						return m, func() tea.Msg { return executeCommand("scale "+val, "", getCurrentDeploymentName(m.items, m.cursor))() }
					case "rollback":
						// Validate rollback revision is a positive integer
						if val == "" {
							m.rawContent = "Revision number cannot be empty"
							m.updateViewportContent()
							return m, nil
						}
						if !isPositiveInteger(val) {
							m.rawContent = "Revision must be a positive integer"
							m.updateViewportContent()
							return m, nil
						}
						helmRelease := getCurrentHelmRelease(m.items, m.cursor, m.helmReleases)
						if helmRelease == "" {
							m.rawContent = "No Helm release found for current deployment"
							m.updateViewportContent()
							return m, nil
						}
						return m, func() tea.Msg { return executeCommand("rollback "+val, helmRelease, "")() }
					case "add":
						val = strings.TrimSpace(val)
						if val == "" {
							m.rawContent = "Deployment name cannot be empty"
							m.updateViewportContent()
							return m, nil
						}
						if !isValidK8sName(val) {
							m.rawContent = "Invalid deployment name. Must be lowercase alphanumeric with hyphens only."
							m.updateViewportContent()
							return m, nil
						}
						return m, func() tea.Msg { return addTargetMsg{name: val} }
					case "remove":
						if len(m.targets) <= 1 {
							m.rawContent = "Cannot remove the last deployment target"
							m.updateViewportContent()
							return m, nil
						}
						if val == "" {
							// Use current deployment
							val = getCurrentDeploymentName(m.items, m.cursor)
						}
						if val != "" {
							return m, func() tea.Msg { return removeTargetMsg{name: val} }
						}
					}
				} else {
					m.textInput.Reset()

					// Special handling for :add and :remove which need to return a Msg, not a Cmd
					parts := strings.Fields(val)
					if len(parts) >= 2 && parts[0] == "add" {
						return m, func() tea.Msg { return addTargetMsg{name: parts[1]} }
					}
					if parts[0] == "remove" {
						var targetToRemove string
						if len(parts) >= 2 {
							targetToRemove = parts[1]
						} else {
							// If no name specified, try to remove current deployment
							targetToRemove = getCurrentDeploymentName(m.items, m.cursor)
							if targetToRemove == "" {
								m.rawContent = "Usage: remove <deployment_name> or select a deployment first"
								m.updateViewportContent()
								return m, nil
							}
						}
						// Check if target exists before removing
						exists := false
						for _, t := range m.targets {
							if t == targetToRemove {
								exists = true
								break
							}
						}
						if !exists {
							m.rawContent = fmt.Sprintf("Target '%s' not found in current deployments", targetToRemove)
							m.updateViewportContent()
							return m, nil
						}
						if len(m.targets) <= 1 {
							m.rawContent = "Cannot remove the last deployment target"
							m.updateViewportContent()
							return m, nil
						}
						return m, func() tea.Msg { return removeTargetMsg{name: targetToRemove} }
					}

					// Find the helm release for current deployment context
					deploymentName := getCurrentDeploymentName(m.items, m.cursor)
					helmRelease := getCurrentHelmRelease(m.items, m.cursor, m.helmReleases)
					cmds = append(cmds, executeCommand(val, helmRelease, deploymentName))
				}
				return m, tea.Batch(cmds...)

			case "esc":
				m.inputMode = false
				m.filterMode = false
				m.shortcutMode = ""
				m.textInput.Blur()
				m.textInput.Reset()
				// Reset autocomplete state
				m.showSuggestions = false
				m.suggestions = []string{}
				m.suggestionIndex = 0
				return m, nil
			}
		}
		// Store old value to detect changes
		oldValue := m.textInput.Value()
		m.textInput, cmd = m.textInput.Update(msg)

		// If text changed in add/remove mode, update suggestions
		if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.textInput.Value() != oldValue {
			m.updateSuggestions()
		}

		return m, cmd
	}

	// --- NORMAL MODE ---
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case ":":
			m.inputMode = true
			m.filterMode = false
			m.textInput.Prompt = ": "
			m.textInput.Placeholder = "scale 3 | restart | add <name> | remove <name>"
			m.textInput.Focus()
			return m, textinput.Blink

		case "/":
			m.inputMode = true
			m.filterMode = true
			m.textInput.Prompt = "/ "
			m.textInput.Placeholder = "Search..."
			m.textInput.SetValue(m.activeFilter)
			m.textInput.Focus()
			m.updateViewportContent()
			return m, textinput.Blink

		case "esc":
			if m.activeFilter != "" {
				m.activeFilter = ""
				m.filterRegex = nil
				m.updateViewportContent()
			}

		case "ctrl+f":
			cmds = append(cmds, fetchDataCmd(m.targets, m.selectors))

		case "f":
			// Toggle log format mode
			m.partialKey = ""
			m.logFormatMode = !m.logFormatMode
			m.updateViewportContent()
			return m, nil

		case "r":
			if m.partialKey == "r" {
				// Double 'r' - execute restart immediately
				m.partialKey = ""
				deploymentName := getCurrentDeploymentName(m.items, m.cursor)
				if deploymentName != "" {
					helmRelease := getCurrentHelmRelease(m.items, m.cursor, m.helmReleases)
					cmds = append(cmds, executeCommand("restart", helmRelease, deploymentName))
				}
			} else {
				// Start of 'r' sequence for 'rr' (restart)
				m.partialKey = "r"
			}

		case "-":
			// Remove shortcut with autocomplete - show currently monitored deployments
			m.partialKey = "" // Clear any partial key
			m.inputMode = true
			m.filterMode = false
			m.shortcutMode = "remove"
			m.textInput.Prompt = "Remove deployment: "
			m.textInput.Placeholder = "Select deployment to remove..."
			m.textInput.Reset()
			m.textInput.Focus()
			// Reset suggestions state and populate with current targets
			m.suggestions = make([]string, len(m.targets))
			copy(m.suggestions, m.targets)
			m.suggestionIndex = 0
			m.showSuggestions = len(m.suggestions) > 0
			return m, textinput.Blink

		case "R":
			// Rollback shortcut (capital R) - prompt for revision
			m.partialKey = "" // Clear any partial key
			m.inputMode = true
			m.filterMode = false
			m.shortcutMode = "rollback"
			m.textInput.Prompt = "Rollback to revision: "
			m.textInput.Placeholder = "Revision number"
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textinput.Blink

		case "s":
			// Scale shortcut - prompt for replicas
			m.partialKey = "" // Clear any partial key
			m.inputMode = true
			m.filterMode = false
			m.shortcutMode = "scale"
			m.textInput.Prompt = "Scale to: "
			m.textInput.Placeholder = "Number of replicas"
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textinput.Blink

		case "+":
			// Add shortcut - prompt for deployment name with autocomplete
			m.partialKey = "" // Clear any partial key
			m.inputMode = true
			m.filterMode = false
			m.shortcutMode = "add"
			m.textInput.Prompt = "Add deployment: "
			m.textInput.Placeholder = "Type to search deployments..."
			m.textInput.Reset()
			m.textInput.Focus()
			// Reset suggestions state
			m.suggestions = []string{}
			m.suggestionIndex = 0
			m.showSuggestions = false
			// Fetch available deployments for autocomplete
			return m, tea.Batch(textinput.Blink, fetchAvailableDeployments())

		case "1", "2", "3", "4", "5":
			m.partialKey = "" // Clear any partial key
			target := ""
			switch msg.String() {
			case "1":
				target = "DEP"
			case "2":
				target = "HELM"
			case "3":
				target = "CM"
			case "4":
				target = "SEC"
			case "5":
				target = "POD"
			}

			// Find next index
			start := 0
			// If we are currently on this type, start searching from next item
			if len(m.items) > 0 && m.items[m.cursor].Type == target {
				start = m.cursor + 1
			}

			found := -1
			// Search forward
			for i := start; i < len(m.items); i++ {
				if m.items[i].Type == target {
					found = i
					break
				}
			}
			// Wrap around if not found
			if found == -1 {
				for i := 0; i < start; i++ {
					if m.items[i].Type == target {
						found = i
						break
					}
				}
			}

			if found != -1 {
				m.cursor = found
				// Adjust scroll
				if m.cursor < m.listOffset {
					m.listOffset = m.cursor
				} else if m.cursor >= m.listOffset+m.listHeight {
					m.listOffset = m.cursor - m.listHeight + 1
				}
				// Refresh details
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.listOffset {
					m.listOffset = m.cursor
				}
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.listOffset+m.listHeight {
					m.listOffset++
				}
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
			}

		case "tab":
			if len(m.items) > 0 {
				curr := m.items[m.cursor]
				if curr.Type == "DEP" {
					// Cycle 0 (YAML) -> 1 (Events) -> 2 (Logs) -> 0
					m.activeTab = (m.activeTab + 1) % DeploymentTabCount
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
				} else if curr.Type == "POD" {
					m.activeTab = (m.activeTab + 1) % PodTabCount
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
				} else {
					// Reset tab for other resource types
					m.activeTab = 0
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
				}
			}

		case "enter":
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
			}

		// Viewport scrolling keybindings
		case "ctrl+d":
			// Scroll viewport down half page (vim-style)
			m.viewport.HalfViewDown()
		case "ctrl+u":
			// Scroll viewport up half page (vim-style)
			m.viewport.HalfViewUp()
		case "ctrl+e":
			// Scroll viewport down one line (vim-style)
			m.viewport.LineDown(1)
		case "ctrl+y":
			// Scroll viewport up one line (vim-style)
			m.viewport.LineUp(1)
		case "pgdown":
			// Scroll viewport down one page
			m.viewport.ViewDown()
		case "pgup":
			// Scroll viewport up one page
			m.viewport.ViewUp()

		case "y":
			// Yank (copy) right pane content to clipboard (vim-style)
			m.partialKey = ""
			return m, yankCmd(m.rawContent)

		default:
			// Clear partial key for any unhandled input
			m.partialKey = ""
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *model) updateViewportContent() {
	content := strings.ReplaceAll(m.rawContent, "\r\n", "\n")

	if m.activeFilter != "" {
		lines := strings.Split(content, "\n")
		filtered := make([]string, 0, len(lines)/10) // Estimate ~10% match rate

		re := m.filterRegex
		if re == nil {
			// Compile and cache the regex
			r, err := regexp.Compile("(?i)" + regexp.QuoteMeta(m.activeFilter))
			if err == nil {
				re = r
				m.filterRegex = r // Cache for future calls
			}
		}

		for _, line := range lines {
			if re != nil && re.MatchString(line) {
				highlighted := re.ReplaceAllStringFunc(line, func(s string) string {
					return styleHighlight.Render(s)
				})
				filtered = append(filtered, highlighted)
			}
		}

		if len(filtered) == 0 {
			content = "No results found for filter: " + m.activeFilter
		} else {
			content = strings.Join(filtered, "\n")
		}
	}

	wrapWidth := m.viewport.Width - 2
	if wrapWidth < MinWrapWidth {
		wrapWidth = MinWrapWidth
	}
	wrapper := lipgloss.NewStyle().Width(wrapWidth)
	m.viewport.SetContent(wrapper.Render(content))
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	leftWidth := int(float64(m.width) * LeftPaneWidthRatio)
	if leftWidth < MinLeftPaneWidth {
		leftWidth = MinLeftPaneWidth
	}

	var listItems []string
	// Header Title
	listItems = append(listItems, styleTitle.Render("K9s Deck"))

	infoLine := fmt.Sprintf("%s | %s", m.lastUpd.Format("15:04:05"), Context)
	if m.err != nil {
		listItems = append(listItems, styleErr.Render("Err: "+m.err.Error()))
	} else {
		listItems = append(listItems, styleDim.Render(infoLine))
	}

	// Show status message if present (e.g., "Yanked to clipboard")
	if m.statusMsg != "" {
		listItems = append(listItems, styleTitle.Render("✓ "+m.statusMsg))
	}

	listItems = append(listItems, "")

	if len(m.items) == 0 {
		listItems = append(listItems, "Loading resources...")
	} else {
		end := m.listOffset + m.listHeight
		if end > len(m.items) {
			end = len(m.items)
		}

		for i := m.listOffset; i < end; i++ {
			if i >= len(m.items) {
				break
			}
			item := m.items[i]

			if item.Type == "HDR" {
				listItems = append(listItems, styleHeader.Render(item.Name))
				continue
			}

			icon := " "
			st := styleDim
			statusStr := ""
			switch item.Type {
			case "DEP":
				icon = "🚀"
				st = styleTitle.Copy()
			case "POD":
				icon = "📦"
				statusStr = fmt.Sprintf("(%s)", item.Status)
				if strings.Contains(item.Status, "Running") && !strings.Contains(item.Status, "0/") {
					st = st.Copy().Foreground(cGreen)
				} else if strings.Contains(item.Status, "Terminating") || strings.Contains(item.Status, "ContainerCreating") || strings.Contains(item.Status, "Pending") || strings.Contains(item.Status, "0/") {
					st = st.Copy().Foreground(cYellow)
				} else {
					st = st.Copy().Foreground(cRed)
				}
			case "HELM":
				icon = "⚓"
				st = st.Copy().Foreground(lipgloss.Color("201"))
			case "SEC":
				icon = "🔒"
				st = st.Copy().Foreground(cYellow)
			case "CM":
				icon = "📜"
				st = st.Copy().Foreground(cSecondary)
			}

			availNameWidth := leftWidth - 9 - len(statusStr) - 2
			if availNameWidth < 5 {
				availNameWidth = 5
			}
			nameDisplay := item.Name
			if len(nameDisplay) > availNameWidth {
				cutLen := availNameWidth - 1
				if cutLen < 0 {
					cutLen = 0
				}
				nameDisplay = nameDisplay[:cutLen] + "…"
			}
			label := fmt.Sprintf("%s %-4s %s %s", icon, item.Type, nameDisplay, statusStr)
			if m.cursor == i {
				listItems = append(listItems, styleSelected.Render(label))
			} else {
				listItems = append(listItems, st.Render(label))
			}
		}
	}
	leftStack := lipgloss.JoinVertical(lipgloss.Left, listItems...)
	leftPane := stylePane.Width(leftWidth).Render(leftStack)

	var tabs string
	if len(m.items) > 0 {
		curr := m.items[m.cursor]
		if curr.Type == "DEP" {
			t1, t2, t3 := styleTabInactive, styleTabInactive, styleTabInactive
			if m.activeTab == 0 {
				t1 = styleTabActive
			}
			if m.activeTab == 1 {
				t2 = styleTabActive
			}
			if m.activeTab == 2 {
				t3 = styleTabActive
			}
			tabs = lipgloss.JoinHorizontal(lipgloss.Top, t1.Render("YAML"), t2.Render("Events"), t3.Render("Logs"))
		} else if curr.Type == "POD" {
			t1, t2 := styleTabInactive, styleTabInactive
			if m.activeTab == 0 {
				t1 = styleTabActive
			} else {
				t2 = styleTabActive
			}
			tabs = lipgloss.JoinHorizontal(lipgloss.Top, t1.Render("YAML"), t2.Render("Logs"))
		} else {
			tabs = styleTabActive.Render("Details")
		}
	} else {
		tabs = styleTabActive.Render("Details")
	}

	rightView := styleBorder.Width(m.viewport.Width).Height(m.viewport.Height).Render(m.viewport.View())
	rightStack := lipgloss.JoinVertical(lipgloss.Left, tabs, rightView)
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightStack)

	var footer string
	if m.inputMode {
		inputView := m.textInput.View()

		// Show suggestions for add/remove mode
		if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.showSuggestions {
			suggestions := m.getFilteredSuggestions()
			if len(suggestions) > 0 {
				var suggestionLines []string
				for i, suggestion := range suggestions {
					prefix := "  "
					if i == m.suggestionIndex {
						prefix = "▶ " // highlight selected suggestion
						suggestion = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(suggestion)
					} else {
						suggestion = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(suggestion)
					}
					suggestionLines = append(suggestionLines, prefix+suggestion)
				}

				suggestionsView := lipgloss.JoinVertical(lipgloss.Left, suggestionLines...)
				action := "Add"
				if m.shortcutMode == "remove" {
					action = "Remove"
				}
				helpLine := styleDim.Render(fmt.Sprintf(" [Tab] Complete  [↑↓] Navigate  [Enter] %s  [Esc] Cancel", action))
				footer = lipgloss.JoinVertical(lipgloss.Left,
					styleCmdBar.Width(m.width).Render(inputView),
					suggestionsView,
					helpLine)
			} else {
				footer = styleCmdBar.Width(m.width).Render(inputView)
			}
		} else {
			footer = styleCmdBar.Width(m.width).Render(inputView)
		}
	} else {
		hint := " [:] Cmds  [/] Filter  [Tab] View  [f] Format  [y] Yank  [Ctrl+d/u] Scroll  [Ctrl-F] Refresh  [rr] Restart  [s] Scale  [R] Rollback  [+] Add  [-] Remove  [q] Quit"

		// Add format mode indicator
		if m.logFormatMode {
			hint += " (Formatted)"
		} else {
			hint += " (Raw)"
		}

		if m.activeFilter != "" {
			hint = fmt.Sprintf(" FILTER: \"%s\" (Esc to clear) | %s", m.activeFilter, hint)
		}
		footer = styleDim.Render(hint)
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, footer)
}

func runCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// fetchAvailableDeployments gets all deployments in the current namespace
func fetchAvailableDeployments() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
		defer cancel()

		deployments, err := client.ListDeployments(ctx, Namespace)
		if err != nil {
			return suggestionsMsg{deployments: []string{}}
		}

		return suggestionsMsg{deployments: deployments}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(TickerInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// stripANSI removes ANSI escape codes from a string
func stripANSI(s string) string {
	// Regex to match ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(s, "")
}

// copyToClipboard copies content to system clipboard (cross-platform)
func copyToClipboard(content string) error {
	// Strip ANSI color codes before copying
	cleanContent := stripANSI(content)

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("clip")
	default:
		return fmt.Errorf("unsupported platform")
	}

	cmd.Stdin = strings.NewReader(cleanContent)
	return cmd.Run()
}

// yankCmd copies the current content to clipboard
func yankCmd(content string) tea.Cmd {
	return func() tea.Msg {
		err := copyToClipboard(content)
		return copyMsg{success: err == nil, err: err}
	}
}

func executeCommand(input, helmRelease, deploymentName string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(input)
		if len(parts) == 0 {
			return nil
		}
		verb := parts[0]

		// :add is handled in Update now via addTargetMsg

		ctx, cancel := context.WithTimeout(context.Background(), LongCommandTimeout)
		defer cancel()

		switch verb {
		case "scale":
			if len(parts) < 2 {
				return detailsMsg{err: fmt.Errorf("Usage: scale <replicas>")}
			}
			if deploymentName == "" {
				return detailsMsg{err: fmt.Errorf("No deployment selected")}
			}
			replicas := 0
			if _, err := fmt.Sscanf(parts[1], "%d", &replicas); err != nil {
				return detailsMsg{err: fmt.Errorf("Invalid replica count: %s", parts[1])}
			}
			err := client.ScaleDeployment(ctx, Namespace, deploymentName, replicas)
			if err != nil {
				return detailsMsg{err: fmt.Errorf("Scale failed: %v", err)}
			}
			return commandFinishedMsg{}
		case "restart":
			if deploymentName == "" {
				return detailsMsg{err: fmt.Errorf("No deployment selected")}
			}
			err := client.RestartDeployment(ctx, Namespace, deploymentName)
			if err != nil {
				return detailsMsg{err: fmt.Errorf("Restart failed: %v", err)}
			}
			return commandFinishedMsg{}
		case "rollback":
			if helmRelease == "" {
				return detailsMsg{err: fmt.Errorf("No Helm release associated.")}
			}
			if len(parts) < 2 {
				return detailsMsg{err: fmt.Errorf("Usage: rollback <revision>")}
			}
			revision := 0
			if _, err := fmt.Sscanf(parts[1], "%d", &revision); err != nil {
				return detailsMsg{err: fmt.Errorf("Invalid revision: %s", parts[1])}
			}
			err := client.RollbackHelm(ctx, Namespace, helmRelease, revision)
			if err != nil {
				return detailsMsg{err: fmt.Errorf("Rollback failed: %v", err)}
			}
			return commandFinishedMsg{}
		case "fetch":
			return tea.Batch(
				func() tea.Msg { return detailsMsg{content: "Manual Refresh...", isYaml: false} },
				func() tea.Msg { return commandFinishedMsg{} },
				tickCmd(),
			)()
		default:
			return detailsMsg{err: fmt.Errorf("Unknown command: %s", verb)}
		}
	}
}

func fetchDataCmd(targets []string, selectors map[string]string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		var mu sync.Mutex

		// Use map to maintain consistent ordering
		targetItems := make(map[string][]item)
		updatedSelectors := make(map[string]string)
		updatedHelm := make(map[string]string)
		var combinedErr error

		for _, targetName := range targets {
			wg.Add(1)
			go func(tName string) {
				defer wg.Done()

				ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
				defer cancel()

				depOut, depErr := client.GetDeployment(ctx, Namespace, tName)

				if depErr != nil {
					mu.Lock()
					targetItems[tName] = []item{{Type: "HDR", Name: fmt.Sprintf("=== %s (Err) ===", tName)}}
					if combinedErr == nil {
						combinedErr = depErr
					}
					mu.Unlock()
					return
				}

				jsonRaw := string(depOut)

				// Collect local items for this deployment
				var localItems []item
				localItems = append(localItems, item{Type: "HDR", Name: fmt.Sprintf("=== %s ===", tName)})
				localItems = append(localItems, item{Type: "DEP", Name: tName, Status: "Active"})

				// Helm
				annotations := gjson.Get(jsonRaw, "metadata.annotations").Map()
				helmName := ""
				if val, ok := annotations["meta.helm.sh/release-name"]; ok {
					helmName = val.String()
				}
				if helmName != "" {
					localItems = append(localItems, item{Type: "HELM", Name: helmName, Status: "Release"})
					mu.Lock()
					updatedHelm[tName] = helmName
					mu.Unlock()
				}

				// Secrets/CM
				seenSecrets := make(map[string]bool)
				seenConfigMaps := make(map[string]bool)

				containers := gjson.Get(jsonRaw, "spec.template.spec.containers").Array()
				for _, c := range containers {
					// Check envFrom
					c.Get("envFrom").ForEach(func(_, v gjson.Result) bool {
						if name := v.Get("secretRef.name").String(); name != "" && !seenSecrets[name] {
							seenSecrets[name] = true
							localItems = append(localItems, item{Type: "SEC", Name: name, Status: "Ref"})
						}
						if name := v.Get("configMapRef.name").String(); name != "" && !seenConfigMaps[name] {
							seenConfigMaps[name] = true
							localItems = append(localItems, item{Type: "CM", Name: name, Status: "Ref"})
						}
						return true
					})
					// Check env
					c.Get("env").ForEach(func(_, v gjson.Result) bool {
						if name := v.Get("valueFrom.secretKeyRef.name").String(); name != "" && !seenSecrets[name] {
							seenSecrets[name] = true
							localItems = append(localItems, item{Type: "SEC", Name: name, Status: "Ref"})
						}
						if name := v.Get("valueFrom.configMapKeyRef.name").String(); name != "" && !seenConfigMaps[name] {
							seenConfigMaps[name] = true
							localItems = append(localItems, item{Type: "CM", Name: name, Status: "Ref"})
						}
						return true
					})
				}

				// Check volumes
				gjson.Get(jsonRaw, "spec.template.spec.volumes").ForEach(func(_, v gjson.Result) bool {
					if name := v.Get("secret.secretName").String(); name != "" && !seenSecrets[name] {
						seenSecrets[name] = true
						localItems = append(localItems, item{Type: "SEC", Name: name, Status: "Ref"})
					}
					if name := v.Get("configMap.name").String(); name != "" && !seenConfigMaps[name] {
						seenConfigMaps[name] = true
						localItems = append(localItems, item{Type: "CM", Name: name, Status: "Ref"})
					}
					return true
				})

				// Pods
				selectorMap := gjson.Get(jsonRaw, "spec.selector.matchLabels").Map()
				keys := make([]string, 0, len(selectorMap))
				for k := range selectorMap {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				labels := make([]string, 0, len(keys))
				for _, k := range keys {
					labels = append(labels, k+"="+selectorMap[k].String())
				}
				newSelector := strings.Join(labels, ",")

				if newSelector != "" {
					mu.Lock()
					updatedSelectors[tName] = newSelector
					mu.Unlock()

					podOut, podErr := client.ListPods(ctx, Namespace, newSelector)
					if podErr == nil {
						gjson.Get(string(podOut), "items").ForEach(func(_, p gjson.Result) bool {
							phase := p.Get("status.phase").String()
							readyCount, totalCount := 0, 0
							p.Get("status.containerStatuses").ForEach(func(_, c gjson.Result) bool {
								totalCount++
								if c.Get("ready").Bool() {
									readyCount++
								}
								return true
							})
							isReady := totalCount > 0 && readyCount == totalCount
							status := phase
							if p.Get("metadata.deletionTimestamp").Exists() {
								status = "Terminating"
							} else if isReady {
								status = "Running"
							} else {
								waitingReason := ""
								p.Get("status.containerStatuses").ForEach(func(_, c gjson.Result) bool {
									if r := c.Get("state.waiting.reason").String(); r != "" {
										waitingReason = r
										return false
									}
									return true
								})
								if waitingReason != "" {
									status = waitingReason
								}
							}
							fullStatus := fmt.Sprintf("%s %d/%d", status, readyCount, totalCount)
							localItems = append(localItems, item{Type: "POD", Name: p.Get("metadata.name").String(), Status: fullStatus})
							return true
						})
					}
				}

				mu.Lock()
				targetItems[tName] = localItems
				mu.Unlock()
			}(targetName)
		}

		wg.Wait()

		// Assemble items in consistent order (sorted by target name)
		var globalItems []item
		sort.Strings(targets) // Ensure consistent target order
		for _, tName := range targets {
			if items, exists := targetItems[tName]; exists {
				globalItems = append(globalItems, items...)
			}
		}

		return dataMsg{items: globalItems, selectors: updatedSelectors, helmReleases: updatedHelm, err: combinedErr}
	}
}

func fetchDetailsCmd(i item, tab int, selectors map[string]string, multiContainerInfo *multiContainerCache) tea.Cmd {
	return func() tea.Msg {
		var out []byte
		var err error
		isYaml := true

		ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
		defer cancel()

		if i.Type == "HDR" {
			return detailsMsg{content: "Service Group: " + i.Name, isYaml: false}
		}

		if i.Type == "DEP" {
			if tab == 1 { // Events
				out, err = client.GetEvents(ctx, Namespace)
				if err != nil {
					return detailsMsg{err: fmt.Errorf("Events error: %v", err)}
				}
				var events []string
				events = append(events, fmt.Sprintf("%-25s %-10s %-15s %s", "TIMESTAMP", "TYPE", "REASON", "MESSAGE"))
				gjson.Get(string(out), "items").ForEach(func(_, e gjson.Result) bool {
					objName := e.Get("involvedObject.name").String()
					if strings.Contains(objName, i.Name) {
						ts := e.Get("lastTimestamp").String()
						if ts == "" {
							ts = e.Get("eventTime").String()
						}
						events = append(events, fmt.Sprintf("%-25s %-10s %-15s %s", ts, e.Get("type").String(), e.Get("reason").String(), e.Get("message").String()))
					}
					return true
				})
				if len(events) == 1 {
					return detailsMsg{content: "No recent events found.", isYaml: false}
				}
				return detailsMsg{content: strings.Join(events, "\n"), isYaml: false}
			} else if tab == 2 { // Aggregated Logs
				// Use cached selector data instead of kubectl call
				selector, exists := selectors[i.Name]
				if !exists || selector == "" {
					return detailsMsg{err: fmt.Errorf("No label selector found for deployment %s", i.Name)}
				}

				// Get logs from all pods using cached label selector
				out, err = runCmd("kubectl", "logs", "-l", selector, "-n", Namespace, "--context", Context, "--all-containers=true", "--prefix", fmt.Sprintf("--tail=%d", DeploymentLogTail))
				if err != nil {
					return detailsMsg{err: fmt.Errorf("Logs Err: %v", err)}
				}
				return detailsMsg{content: string(out), isYaml: false}
			}
		}

		if i.Type == "POD" && tab == 1 {
			// Detect if pod has multiple containers
			isMulti, detectionErr := detectMultiContainer(i.Name, multiContainerInfo)

			// Use client to get pod logs
			prefix := detectionErr == nil && isMulti
			out, err = client.GetPodLogs(ctx, Namespace, i.Name, DefaultLogTailLines, true, prefix)
			if err != nil {
				return detailsMsg{err: fmt.Errorf("Log error: %v", err)}
			}
			return detailsMsg{content: string(out), isYaml: false}
		}

		if i.Type == "SEC" {
			out, err = client.GetSecret(ctx, Namespace, i.Name)
			if err == nil {
				dataMap := gjson.Get(string(out), "data").Map()
				decoded := make(map[string]string)
				for k, v := range dataMap {
					val, _ := base64.StdEncoding.DecodeString(v.String())
					decoded[k] = string(val)
				}
				pretty, _ := json.MarshalIndent(decoded, "", "  ")
				return detailsMsg{content: string(pretty), isYaml: true}
			}
		} else if i.Type == "HELM" {
			out, err = client.GetHelmHistory(ctx, Namespace, i.Name)
			isYaml = false
		} else if i.Type == "CM" {
			out, err = client.GetConfigMap(ctx, Namespace, i.Name)
		} else if i.Type == "DEP" {
			// For deployment YAML view (tab == 0)
			out, err = client.GetDeployment(ctx, Namespace, i.Name)
			if err == nil {
				// Pretty-print the JSON for readability
				var prettyJSON bytes.Buffer
				if jsonErr := json.Indent(&prettyJSON, out, "", "  "); jsonErr == nil {
					out = prettyJSON.Bytes()
				}
			}
			isYaml = true
		} else {
			// For POD YAML, use kubectl for now (no GetPod method yet)
			out, err = runCmd("kubectl", "get", "pod", i.Name, "-n", Namespace, "--context", Context, "-o", "yaml")
		}

		if err != nil {
			return detailsMsg{err: fmt.Errorf("%s\n%s", err.Error(), string(out))}
		}
		return detailsMsg{content: string(out), isYaml: isYaml}
	}
}

func highlight(content, format string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, format, "terminal256", "dracula")
	if err != nil {
		return content
	}
	return buf.String()
}

func getCurrentDeploymentName(items []item, cursor int) string {
	if len(items) == 0 || cursor >= len(items) {
		return ""
	}
	curr := items[cursor]
	if curr.Type == "DEP" {
		return curr.Name
	}
	// Find the deployment this resource belongs to
	for i := cursor; i >= 0; i-- {
		if items[i].Type == "DEP" {
			return items[i].Name
		}
	}
	return ""
}

func getCurrentHelmRelease(items []item, cursor int, helmReleases map[string]string) string {
	deploymentName := getCurrentDeploymentName(items, cursor)
	if deploymentName == "" {
		return ""
	}
	return helmReleases[deploymentName]
}

// --- VALIDATION HELPERS ---

func isPositiveInteger(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isValidK8sName(name string) bool {
	if name == "" || len(name) > MaxK8sNameLength {
		return false
	}
	// K8s names must be lowercase alphanumeric with hyphens
	// Cannot start or end with hyphen
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

// updateSuggestions filters the available suggestions based on current input
func (m *model) updateSuggestions() {
	if (m.shortcutMode != "add" && m.shortcutMode != "remove") || len(m.suggestions) == 0 {
		m.showSuggestions = false
		return
	}

	input := strings.ToLower(strings.TrimSpace(m.textInput.Value()))
	if input == "" {
		m.showSuggestions = true
		m.suggestionIndex = 0
		return
	}

	// Filter suggestions that contain the input
	filtered := make([]string, 0, len(m.suggestions))

	// Build a map of targets for O(1) lookup instead of O(n)
	targetMap := make(map[string]bool, len(m.targets))
	for _, target := range m.targets {
		targetMap[target] = true
	}

	for _, suggestion := range m.suggestions {
		if strings.Contains(strings.ToLower(suggestion), input) {
			if m.shortcutMode == "add" {
				// For add mode: Don't suggest deployments already being monitored
				if !targetMap[suggestion] {
					filtered = append(filtered, suggestion)
				}
			} else if m.shortcutMode == "remove" {
				// For remove mode: Only suggest currently monitored deployments
				filtered = append(filtered, suggestion)
			}
		}
	}

	m.suggestions = filtered
	m.showSuggestions = len(filtered) > 0
	m.suggestionIndex = 0
}

// getFilteredSuggestions returns suggestions for display (limited to MaxSuggestions)
func (m *model) getFilteredSuggestions() []string {
	if !m.showSuggestions || len(m.suggestions) == 0 {
		return []string{}
	}

	if len(m.suggestions) <= MaxSuggestions {
		return m.suggestions
	}
	return m.suggestions[:MaxSuggestions]
}

// --- LOG PROCESSING FUNCTIONS ---

// parseLogLine extracts components from a log line
func parseLogLine(line string) logLineInfo {
	info := logLineInfo{
		OriginalLine: line,
		LogContent:   line,
	}

	// Try to extract pod prefix [pod/podname/container] or [podname/container]
	if matches := podPrefixRegex.FindStringSubmatch(line); len(matches) == 5 {
		// kubectl --prefix format: [pod/podname/container]
		info.PodPrefix = matches[2] + "/" + matches[3]
		info.PodName = matches[2]
		info.ContainerName = matches[3]
		info.LogContent = matches[4]
	}

	// Detect log level
	if levelMatches := logLevelRegex.FindStringSubmatch(info.LogContent); len(levelMatches) > 1 {
		info.LogLevel = strings.ToUpper(levelMatches[1])
	}

	// Detect JSON
	trimmed := strings.TrimSpace(info.LogContent)
	info.IsJSON = (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))

	return info
}

// getPodColor returns a consistent color for a pod name using hash
func getPodColor(podName string) lipgloss.Color {
	hash := 0
	for _, c := range podName {
		hash = (hash*31 + int(c)) % len(podColorPalette)
	}
	if hash < 0 {
		hash = -hash
	}
	return podColorPalette[hash%len(podColorPalette)]
}

// getLogLevelColor returns the color for a log level
func getLogLevelColor(level string) lipgloss.Color {
	normalized := strings.ToUpper(strings.TrimSpace(level))
	switch normalized {
	case "FATAL", "ERROR", "ERR":
		return cRed
	case "WARN", "WARNING":
		return cYellow
	case "INFO":
		return lipgloss.Color("39") // Cyan
	case "DEBUG":
		return cGray
	case "TRACE":
		return lipgloss.Color("238") // Darker gray
	default:
		return lipgloss.Color("255") // Default white
	}
}

// shortenPodPrefix extracts replicaset hash and pod suffix
func shortenPodPrefix(podName, containerName string) string {
	// Pod format: deployment-replicasethash-podhash
	// Example: third-service-55c74d7f8-zn5fd
	// We want: [55c74d7f8-zn5fd]
	// (deployment name is redundant since we're already viewing that deployment)

	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		// If pod name doesn't follow expected format, return as-is
		return fmt.Sprintf("[%s]", podName)
	}

	// Extract replicaset hash (second to last part)
	replicaSetHash := parts[len(parts)-2]

	// Extract unique pod suffix (last part)
	podSuffix := parts[len(parts)-1]

	return fmt.Sprintf("[%s-%s]", replicaSetHash, podSuffix)
}

// formatPodPrefix formats pod prefix with color and icon
func formatPodPrefix(podName, containerName string) string {
	shortened := shortenPodPrefix(podName, containerName)
	color := getPodColor(podName)
	icon := "●"

	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	return style.Render(icon + " " + shortened)
}

// colorizeLogLevel applies color to log level keywords in a line
func colorizeLogLevel(line string) string {
	matches := logLevelRegex.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}

	var result strings.Builder
	lastIndex := 0

	for _, match := range matches {
		start, end := match[0], match[1]

		// Write content before match
		result.WriteString(line[lastIndex:start])

		// Colorize the level
		level := line[start:end]
		color := getLogLevelColor(level)
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		result.WriteString(style.Render(level))

		lastIndex = end
	}

	// Write remaining content
	result.WriteString(line[lastIndex:])
	return result.String()
}

// detectJSONLog checks if a line is JSON format
func detectJSONLog(line string) bool {
	trimmed := strings.TrimSpace(line)
	return (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))
}

// prettyPrintJSONLog formats and highlights JSON logs
func prettyPrintJSONLog(line string) string {
	// Try to parse and pretty-print JSON
	var obj interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return line // Return original if not valid JSON
	}

	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return line
	}

	// Apply Chroma syntax highlighting
	return highlight(string(pretty), "json")
}

// detectMultiContainer checks if a pod has multiple containers (with caching)
func detectMultiContainer(podName string, cache *multiContainerCache) (bool, error) {
	// Check cache first
	cache.mu.RLock()
	if result, exists := cache.cache[podName]; exists {
		cache.mu.RUnlock()
		return result, nil
	}
	cache.mu.RUnlock()

	// Query via client
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()

	containerNames, err := client.GetPodContainers(ctx, Namespace, podName)
	if err != nil {
		return false, err
	}

	isMulti := len(containerNames) > 1

	// Cache result
	cache.mu.Lock()
	cache.cache[podName] = isMulti
	cache.mu.Unlock()

	return isMulti, nil
}

// processLogContent is the master log processing function
func processLogContent(content, resourceType, resourceName string, formatMode bool) string {
	if !formatMode {
		return content // Raw mode - return unchanged
	}

	lines := strings.Split(content, "\n")
	processed := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			processed = append(processed, line)
			continue
		}

		// Parse line structure
		info := parseLogLine(line)

		// Check if JSON
		if detectJSONLog(info.LogContent) {
			// Format as JSON
			formatted := prettyPrintJSONLog(info.LogContent)
			if info.PodPrefix != "" {
				prefix := formatPodPrefix(info.PodName, info.ContainerName)
				processed = append(processed, prefix+" "+formatted)
			} else {
				processed = append(processed, formatted)
			}
		} else {
			// Standard text log with level coloring
			formattedLine := line

			// Add pod prefix formatting if present
			if info.PodPrefix != "" {
				prefix := formatPodPrefix(info.PodName, info.ContainerName)
				colorizedContent := colorizeLogLevel(info.LogContent)
				formattedLine = prefix + " " + colorizedContent
			} else {
				formattedLine = colorizeLogLevel(line)
			}

			processed = append(processed, formattedLine)
		}
	}

	return strings.Join(processed, "\n")
}
