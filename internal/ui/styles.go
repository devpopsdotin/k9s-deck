package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// UI Layout Constants
const (
	// Timing
	RefreshInterval = 1 * time.Second
	TickerInterval  = 1 * time.Second

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

	// Tabs
	DeploymentTabCount = 3
	PodTabCount        = 2
)

// Color definitions
var (
	CPrimary   = lipgloss.Color("62")  // Purple/Blue
	CSecondary = lipgloss.Color("39")  // Cyan
	CGreen     = lipgloss.Color("42")  // Green
	CRed       = lipgloss.Color("196") // Red
	CYellow    = lipgloss.Color("220") // Yellow
	CGray      = lipgloss.Color("240") // Gray
)

// Lipgloss styles
var (
	StyleBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(CGray)
	StylePane     = lipgloss.NewStyle().Padding(0, 1)
	StyleTitle    = lipgloss.NewStyle().Foreground(CSecondary).Bold(true)
	StyleSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(CPrimary).Bold(true).Padding(0, 1)
	StyleDim      = lipgloss.NewStyle().Foreground(CGray)
	StyleErr      = lipgloss.NewStyle().Foreground(CRed)
	StyleHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true).Background(lipgloss.Color("237")).Padding(0, 1).Width(100)

	StyleTabActive   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(CPrimary).Foreground(CPrimary).Bold(true).Padding(0, 1)
	StyleTabInactive = lipgloss.NewStyle().Padding(0, 1).Foreground(CGray)

	StyleCmdBar = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("236")).Padding(0, 1)

	StyleHighlight = lipgloss.NewStyle().Background(lipgloss.Color("201")).Foreground(lipgloss.Color("255")).Bold(true)
)
