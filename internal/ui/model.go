package ui

import (
	"regexp"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/devpopsdotin/k9s-deck/internal/state"
)

// Model is the main Bubble Tea model for the UI
type Model struct {
	Items []Item

	// Multi-deployment support
	Targets []string // List of deployments to monitor

	// State management (thread-safe)
	StateManager       *state.Manager
	MultiContainerInfo *state.MultiContainerCache

	// List view
	Cursor     int
	ListOffset int
	ListHeight int

	// Tabs and views
	ActiveTab int

	// Input handling
	TextInput    textinput.Model
	InputMode    bool
	FilterMode   bool
	ShortcutMode string // "scale", "rollback", "add", "remove", or ""
	PartialKey   string // for multi-character shortcuts like "rr"
	ActiveFilter string
	FilterRegex  *regexp.Regexp

	// LSP-like autocomplete
	Suggestions     []string // Available deployment names for autocomplete
	SuggestionIndex int      // Currently selected suggestion
	ShowSuggestions bool     // Whether to show autocomplete suggestions

	// Viewport for details pane
	Viewport   viewport.Model
	RawContent string
	Ready      bool

	// Window dimensions
	Width  int
	Height int

	// State
	LastUpd time.Time
	Err     error

	// Log formatting
	LogFormatMode bool // true=formatted, false=raw

	// Status messages
	StatusMsg string // temporary status message (e.g., "Copied to clipboard")

	// Kubernetes context
	Context   string
	Namespace string
}

// NewModel creates a new UI model
func NewModel(context, namespace string, initialDeployment string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type command..."
	ti.CharLimit = 100
	ti.Width = 50

	return Model{
		Context:            context,
		Namespace:          namespace,
		Targets:            []string{initialDeployment},
		StateManager:       state.NewManager(),
		MultiContainerInfo: state.NewMultiContainerCache(),
		TextInput:          ti,
		LogFormatMode:      true, // Default to formatted logs
		Items:              []Item{},
		Suggestions:        []string{},
	}
}
