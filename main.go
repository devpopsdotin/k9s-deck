package main

import (
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

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tidwall/gjson"
	"sigs.k8s.io/yaml"

	"github.com/devpopsdotin/k9s-deck/internal/k8s"
	"github.com/devpopsdotin/k9s-deck/internal/logger"
	"github.com/devpopsdotin/k9s-deck/internal/parser"
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

// --- DATA MODEL ---
type item struct {
	Type   string // DEP, POD, HELM, SEC, CM, HDR
	Name   string
	Status string
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
	allSuggestions  []string // Full candidate list for autocomplete (unfiltered)
	suggestions     []string // Candidates matching the current input
	suggestionIndex int      // Currently selected suggestion
	showSuggestions bool     // Whether to show autocomplete suggestions

	viewport   viewport.Model
	rawContent string
	ready      bool
	width      int
	height     int
	lastUpd    time.Time
	err        error

	// Refresh sequencing: fetchSeq is the last fetch issued, appliedSeq the
	// last result applied. They differ while a fetch is in flight.
	fetchSeq   int
	appliedSeq int

	// Log formatting
	logFormatMode      bool                 // true=formatted, false=raw
	logSource          string               // unprocessed logs currently shown ("" if not a log view)
	multiContainerInfo *multiContainerCache // cache for multi-container detection

	// Status messages
	statusMsg string // temporary status message (e.g., "Copied to clipboard")
}

// --- MESSAGES ---
type tickMsg time.Time
type dataMsg struct {
	seq          int
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

	// Initialize logger (writes to logger.LogPath(), /tmp/k9s-deck.log on Unix)
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
		fetchSeq: 1, // Init issues fetch #1
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.targets, m.fetchSeq), tickCmd(), textinput.Blink)
}

// startFetch issues a new data refresh tagged with the next sequence number
func (m *model) startFetch() tea.Cmd {
	m.fetchSeq++
	return fetchDataCmd(m.targets, m.fetchSeq)
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

// clampListOffset keeps the list scroll offset valid after the item count or
// list height changes: the cursor stays visible and no blank rows are left
// below the last item.
func (m *model) clampListOffset() {
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	} else if m.cursor >= m.listOffset+m.listHeight {
		m.listOffset = m.cursor - m.listHeight + 1
	}
	m.listOffset = minInt(m.listOffset, maxInt(len(m.items)-m.listHeight, 0))
	m.listOffset = maxInt(m.listOffset, 0)
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
		// Skip this tick's refresh if the previous one is still in flight,
		// so slow API responses don't pile up requests
		if m.appliedSeq < m.fetchSeq {
			return m, tickCmd()
		}
		return m, tea.Batch(m.startFetch(), tickCmd())

	case commandFinishedMsg:
		return m, m.startFetch()

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
		return m, m.startFetch()

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
		return m, m.startFetch()

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
			m.allSuggestions = filtered
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
		m.clampListOffset()

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
		// Drop results that arrive after a newer fetch was already applied
		if msg.seq <= m.appliedSeq {
			return m, nil
		}
		m.appliedSeq = msg.seq
		m.lastUpd = time.Now()
		// Record any error, but still apply the items: a failing target is
		// rendered as an "(Err)" header and must not freeze the other targets.
		m.err = msg.err

		// Remember current selection before updating items
		var currentSelection *item
		if len(m.items) > 0 && m.cursor < len(m.items) {
			currentSelection = &m.items[m.cursor]
		}

		m.items = msg.items
		m.multiContainerInfo.prune(m.items)
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

		m.clampListOffset()

		// Always refresh details - pass a copy of selectors to avoid race
		if len(m.items) > 0 {
			cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, copySelectorMap(m.selectors), m.multiContainerInfo))
		}
		return m, tea.Batch(cmds...)

	case detailsMsg:
		m.logSource = ""
		if msg.err != nil {
			m.rawContent = fmt.Sprintf("Error: %v", msg.err)
		} else {
			if msg.isYaml {
				m.rawContent = parser.Highlight(msg.content, "yaml")
			} else {
				// Determine if this is log content
				currentItem := item{}
				if len(m.items) > 0 && m.cursor < len(m.items) {
					currentItem = m.items[m.cursor]
				}

				isLogContent := (currentItem.Type == "DEP" && m.activeTab == 2) ||
					(currentItem.Type == "POD" && m.activeTab == 1)

				if isLogContent {
					m.logSource = msg.content
					m.rawContent = parser.ProcessLogContent(msg.content, currentItem.Type,
						currentItem.Name, m.logFormatMode, parser.Highlight)
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
					// Enter accepts the highlighted suggestion when the list is shown
					if (m.shortcutMode == "add" || m.shortcutMode == "remove") && m.showSuggestions &&
						m.suggestionIndex < len(m.suggestions) {
						val = m.suggestions[m.suggestionIndex]
					}
					m.showSuggestions = false

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
						if !isNonNegativeInteger(val) {
							m.rawContent = "Scale value must be a non-negative integer"
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
					if len(parts) == 0 {
						// Empty command - nothing to do
						return m, nil
					}
					if len(parts) >= 2 && parts[0] == "add" {
						name := parts[1]
						if !isValidK8sName(name) {
							m.rawContent = "Invalid deployment name. Must be lowercase alphanumeric with hyphens only."
							m.updateViewportContent()
							return m, nil
						}
						return m, func() tea.Msg { return addTargetMsg{name: name} }
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
				m.allSuggestions = []string{}
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
			cmds = append(cmds, m.startFetch())

		case "f":
			// Toggle log format mode
			m.partialKey = ""
			m.logFormatMode = !m.logFormatMode
			// Re-render the current logs now rather than at the next refresh
			if m.logSource != "" && len(m.items) > 0 {
				curr := m.items[m.cursor]
				m.rawContent = parser.ProcessLogContent(m.logSource, curr.Type, curr.Name, m.logFormatMode, parser.Highlight)
			}
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
			m.allSuggestions = append([]string(nil), m.targets...)
			m.suggestions = append([]string(nil), m.targets...)
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
			m.allSuggestions = []string{}
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
			suggestions, offset := m.getFilteredSuggestions()
			if len(suggestions) > 0 {
				var suggestionLines []string
				for i, suggestion := range suggestions {
					prefix := "  "
					if offset+i == m.suggestionIndex {
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
	case "linux", "freebsd", "openbsd", "netbsd":
		// Prefer the Wayland tool on Wayland sessions, then the X11 tools
		var candidates [][]string
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		candidates = append(candidates,
			[]string{"xclip", "-selection", "clipboard"},
			[]string{"xsel", "--clipboard", "--input"})
		for _, c := range candidates {
			if _, err := exec.LookPath(c[0]); err == nil {
				cmd = exec.Command(c[0], c[1:]...)
				break
			}
		}
		if cmd == nil {
			return fmt.Errorf("no clipboard tool found (install wl-copy, xclip or xsel)")
		}
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
			if _, err := fmt.Sscanf(parts[1], "%d", &replicas); err != nil || replicas < 0 {
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
			// No tickCmd here: the existing tick loop keeps running, and starting
			// another would permanently add a second refresh loop
			return tea.Batch(
				func() tea.Msg { return detailsMsg{content: "Manual Refresh...", isYaml: false} },
				func() tea.Msg { return commandFinishedMsg{} },
			)()
		default:
			return detailsMsg{err: fmt.Errorf("Unknown command: %s", verb)}
		}
	}
}

func fetchDataCmd(targets []string, seq int) tea.Cmd {
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
						combinedErr = fmt.Errorf("%s: %w", tName, depErr)
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
		// Sort a copy: targets shares its backing array with the model, which
		// the UI goroutine reads and appends to concurrently
		sortedTargets := append([]string(nil), targets...)
		sort.Strings(sortedTargets) // Ensure consistent target order
		for _, tName := range sortedTargets {
			if items, exists := targetItems[tName]; exists {
				globalItems = append(globalItems, items...)
			}
		}

		return dataMsg{seq: seq, items: globalItems, selectors: updatedSelectors, helmReleases: updatedHelm, err: combinedErr}
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
					kind := e.Get("involvedObject.kind").String()
					objName := e.Get("involvedObject.name").String()
					if isDeploymentEvent(kind, objName, i.Name) {
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
				logs, err := fetchSelectorLogs(ctx, selector, DeploymentLogTail)
				if err != nil {
					return detailsMsg{err: fmt.Errorf("Logs Err: %v", err)}
				}
				return detailsMsg{content: logs, isYaml: false}
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
				// The client returns JSON; show it as YAML like the other views
				if yamlOut, yamlErr := yaml.JSONToYAML(out); yamlErr == nil {
					out = yamlOut
				}
			}
			isYaml = true
		} else {
			// POD YAML (tab == 0)
			out, err = client.GetPod(ctx, Namespace, i.Name)
		}

		if err != nil {
			return detailsMsg{err: fmt.Errorf("%s\n%s", err.Error(), string(out))}
		}
		return detailsMsg{content: string(out), isYaml: isYaml}
	}
}

// fetchSelectorLogs returns the prefixed logs of all containers of all pods
// matching selector (the client-go equivalent of
// kubectl logs -l <selector> --all-containers --prefix)
func fetchSelectorLogs(ctx context.Context, selector string, tailLines int) (string, error) {
	podsOut, err := client.ListPods(ctx, Namespace, selector)
	if err != nil {
		return "", err
	}
	var podNames []string
	gjson.Get(string(podsOut), "items.#.metadata.name").ForEach(func(_, v gjson.Result) bool {
		podNames = append(podNames, v.String())
		return true
	})
	sort.Strings(podNames)

	// Fetch pods concurrently so many replicas fit within the timeout
	results := make([][]byte, len(podNames))
	errs := make([]error, len(podNames))
	var wg sync.WaitGroup
	for idx, name := range podNames {
		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			results[idx], errs[idx] = client.GetPodLogs(ctx, Namespace, name, tailLines, true, true)
		}(idx, name)
	}
	wg.Wait()

	var sb strings.Builder
	var firstErr error
	for idx := range podNames {
		if errs[idx] != nil {
			if firstErr == nil {
				firstErr = errs[idx]
			}
			continue
		}
		sb.Write(results[idx])
	}
	// Only fail when there were pods and none of them returned logs
	if sb.Len() == 0 && firstErr != nil {
		return "", firstErr
	}
	return sb.String(), nil
}

// Generated name suffixes: ReplicaSets are "<deployment>-<pod-template-hash>",
// Pods are "<deployment>-<pod-template-hash>-<random>"
var (
	replicaSetSuffixRegex = regexp.MustCompile(`^[a-z0-9]{6,10}$`)
	podSuffixRegex        = regexp.MustCompile(`^[a-z0-9]{6,10}-[a-z0-9]{5}$`)
)

// isDeploymentEvent reports whether an event's involved object is the
// deployment itself or one of its ReplicaSets/Pods. A plain substring match
// would also pick up events of other deployments sharing a name prefix.
func isDeploymentEvent(kind, objName, deployment string) bool {
	switch kind {
	case "Deployment":
		return objName == deployment
	case "ReplicaSet", "Pod":
		suffix, ok := strings.CutPrefix(objName, deployment+"-")
		if !ok {
			return false
		}
		if kind == "ReplicaSet" {
			return replicaSetSuffixRegex.MatchString(suffix)
		}
		return podSuffixRegex.MatchString(suffix)
	}
	return false
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
	return isNonNegativeInteger(s) && strings.Trim(s, "0") != ""
}

func isNonNegativeInteger(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
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
	if (m.shortcutMode != "add" && m.shortcutMode != "remove") || len(m.allSuggestions) == 0 {
		m.suggestions = []string{}
		m.showSuggestions = false
		return
	}

	input := strings.ToLower(strings.TrimSpace(m.textInput.Value()))

	// Always filter from the full list so deleting input restores candidates
	filtered := make([]string, 0, len(m.allSuggestions))

	// Build a map of targets for O(1) lookup instead of O(n)
	targetMap := make(map[string]bool, len(m.targets))
	for _, target := range m.targets {
		targetMap[target] = true
	}

	for _, suggestion := range m.allSuggestions {
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

// getFilteredSuggestions returns the window of suggestions to display (at
// most MaxSuggestions) and the index of its first entry in m.suggestions.
// The window scrolls so the selected suggestion is always visible.
func (m *model) getFilteredSuggestions() ([]string, int) {
	if !m.showSuggestions || len(m.suggestions) == 0 {
		return []string{}, 0
	}

	if len(m.suggestions) <= MaxSuggestions {
		return m.suggestions, 0
	}
	start := 0
	if m.suggestionIndex >= MaxSuggestions {
		start = m.suggestionIndex - MaxSuggestions + 1
	}
	return m.suggestions[start : start+MaxSuggestions], start
}

// prune drops cached entries for pods that are no longer listed, so the
// cache doesn't grow forever as pods are replaced
func (c *multiContainerCache) prune(items []item) {
	live := make(map[string]bool)
	for _, it := range items {
		if it.Type == "POD" {
			live[it.Name] = true
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for podName := range c.cache {
		if !live[podName] {
			delete(c.cache, podName)
		}
	}
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
