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
)

// --- CONFIG ---
var (
	Context    string
	Namespace  string
	Deployment string
)

// --- STYLES ---
var (
	cPrimary   = lipgloss.Color("62")  // Purple/Blue
	cSecondary = lipgloss.Color("39")  // Cyan
	cGreen     = lipgloss.Color("42")  // Green
	cRed       = lipgloss.Color("196") // Red
	cYellow    = lipgloss.Color("220") // Yellow
	cGray      = lipgloss.Color("240") // Gray

	styleBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(cGray)
	stylePane   = lipgloss.NewStyle().Padding(0, 1)
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

func init() {
	_ = styles.Get("dracula")
}

// --- DATA MODEL ---
type item struct {
	Type   string // DEP, POD, HELM, SEC, CM, HDR
	Name   string
	Status string
}

type model struct {
	items       []item
	
	targets     []string            // List of deployments to monitor
	selectors   map[string]string   // Cache label selectors per deployment
	helmReleases map[string]string  // Cache helm release names

	cursor      int
	listOffset  int
	listHeight  int

	activeTab    int 
	textInput    textinput.Model
	inputMode    bool
	filterMode   bool          
	shortcutMode string        // "scale", "rollback", "add", "remove", or ""
	partialKey   string        // for multi-character shortcuts like "rm"
	activeFilter string       
	filterRegex  *regexp.Regexp 
	
	viewport    viewport.Model
	rawContent  string        
	ready       bool
	width       int
	height      int
	lastUpd     time.Time
	err         error
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
		textInput:  ti,
		inputMode:  false,
		listHeight: 20,
		targets:    []string{Deployment},
		selectors:  make(map[string]string),
		helmReleases: make(map[string]string),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(m.targets, m.selectors), tickCmd(), textinput.Blink)
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
			if t == msg.name { exists = true; break }
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

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		headerHeight := 3 
		footerHeight := 1
		m.listHeight = msg.Height - headerHeight - footerHeight - 2
		if m.listHeight < 1 { m.listHeight = 1 }

		paneWidth := int(float64(msg.Width) * 0.35) 
		vpWidth := msg.Width - paneWidth - 4
		if vpWidth < 0 { vpWidth = 0 }
		vpHeight := msg.Height - headerHeight - footerHeight - 2 
		if vpHeight < 0 { vpHeight = 0 }
		
		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.YPosition = headerHeight + 1
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
			for k, v := range msg.selectors { m.selectors[k] = v }
			for k, v := range msg.helmReleases { m.helmReleases[k] = v }
			
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
					if m.cursor >= len(m.items) {
						m.cursor = len(m.items) - 1
					}
				}
			} else {
				// Validate cursor position for new or empty selections
				if len(m.items) > 0 && m.cursor >= len(m.items) {
					m.cursor = len(m.items) - 1
					if m.cursor < 0 { m.cursor = 0 }
				}
			}
			
			// Always refresh details
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, m.selectors))
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
				m.rawContent = msg.content
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
			case "enter":
				val := m.textInput.Value()
				m.inputMode = false
				m.textInput.Blur()
				
				if m.filterMode {
					m.activeFilter = val
					if val != "" {
						re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(val))
						if err == nil { m.filterRegex = re }
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
						if val == "" {
							// Use current deployment
							if len(m.items) > 0 {
								curr := m.items[m.cursor]
								if curr.Type == "DEP" {
									val = curr.Name
								} else {
									for i := m.cursor; i >= 0; i-- {
										if m.items[i].Type == "DEP" {
											val = m.items[i].Name
											break
										}
									}
								}
							}
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
							if len(m.items) > 0 {
								curr := m.items[m.cursor]
								if curr.Type == "DEP" {
									targetToRemove = curr.Name
								} else {
									// Find the deployment this resource belongs to
									for i := m.cursor; i >= 0; i-- {
										if m.items[i].Type == "DEP" {
											targetToRemove = m.items[i].Name
											break
										}
									}
								}
							}
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
					helmRelease := ""
					deploymentName := ""
					if len(m.items) > 0 {
						curr := m.items[m.cursor]
						if curr.Type == "DEP" {
							deploymentName = curr.Name
							helmRelease = m.helmReleases[curr.Name]
						} else {
							// Find the deployment this resource belongs to
							for i := m.cursor; i >= 0; i-- {
								if m.items[i].Type == "DEP" {
									deploymentName = m.items[i].Name
									helmRelease = m.helmReleases[m.items[i].Name]
									break
								}
							}
						}
					}
					cmds = append(cmds, executeCommand(val, helmRelease, deploymentName))
				}
				return m, tea.Batch(cmds...)
				
			case "esc":
				m.inputMode = false
				m.filterMode = false
				m.shortcutMode = ""
				m.textInput.Blur()
				m.textInput.Reset()
				return m, nil
			}
		}
		m.textInput, cmd = m.textInput.Update(msg)
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
				// Start of 'r' sequence - could be 'rr' (restart) or 'rm' (remove)
				m.partialKey = "r"
			}

		case "m":
			if m.partialKey == "r" {
				// Complete 'rm' sequence - remove shortcut
				m.partialKey = ""
				m.inputMode = true
				m.filterMode = false
				m.shortcutMode = "remove"
				m.textInput.Prompt = "Remove deployment: "
				m.textInput.Placeholder = "Deployment name (empty for current)"
				m.textInput.Reset()
				m.textInput.Focus()
				return m, textinput.Blink
			}

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
			// Add shortcut - prompt for deployment name
			m.partialKey = "" // Clear any partial key
			m.inputMode = true
			m.filterMode = false
			m.shortcutMode = "add"
			m.textInput.Prompt = "Add deployment: "
			m.textInput.Placeholder = "Deployment name"
			m.textInput.Reset()
			m.textInput.Focus()
			return m, textinput.Blink

		case "1", "2", "3", "4", "5":
			m.partialKey = "" // Clear any partial key
			target := ""
			switch msg.String() {
			case "1": target = "DEP"
			case "2": target = "HELM"
			case "3": target = "CM"
			case "4": target = "SEC"
			case "5": target = "POD"
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
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, m.selectors))
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.listOffset { m.listOffset = m.cursor }
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, m.selectors))
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.listOffset + m.listHeight { m.listOffset++ }
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, m.selectors))
			}

		case "tab":
			if len(m.items) > 0 {
				curr := m.items[m.cursor]
				if curr.Type == "DEP" {
					// Cycle 0 (YAML) -> 1 (Events) -> 2 (Logs) -> 0
					m.activeTab = (m.activeTab + 1) % 3
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, m.selectors))
				} else if curr.Type == "POD" {
					m.activeTab = (m.activeTab + 1) % 2
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, m.selectors))
				} else {
					// Reset tab for other resource types
					m.activeTab = 0
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab, m.selectors))
				}
			}

		case "enter":
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab, m.selectors))
			}
			
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
		var filtered []string
		
		re := m.filterRegex
		if re == nil {
			r, err := regexp.Compile("(?i)" + regexp.QuoteMeta(m.activeFilter))
			if err == nil { re = r }
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
	if wrapWidth < 10 { wrapWidth = 10 }
	wrapper := lipgloss.NewStyle().Width(wrapWidth)
	m.viewport.SetContent(wrapper.Render(content))
}

func (m model) View() string {
	if !m.ready { return "Initializing..." }

	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 20 { leftWidth = 20 }
	
	var listItems []string
	// Header Title
	listItems = append(listItems, styleTitle.Render("K9s Deck"))
	
	infoLine := fmt.Sprintf("%s | %s", m.lastUpd.Format("15:04:05"), Context)
	if m.err != nil {
		listItems = append(listItems, styleErr.Render("Err: " + m.err.Error()))
	} else {
		listItems = append(listItems, styleDim.Render(infoLine))
	}
	listItems = append(listItems, "")

	if len(m.items) == 0 {
		listItems = append(listItems, "Loading resources...")
	} else {
		end := m.listOffset + m.listHeight
		if end > len(m.items) { end = len(m.items) }

		for i := m.listOffset; i < end; i++ {
			if i >= len(m.items) { break }
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
			if availNameWidth < 5 { availNameWidth = 5 } 
			nameDisplay := item.Name
			if len(nameDisplay) > availNameWidth {
			    cutLen := availNameWidth - 1
			    if cutLen < 0 { cutLen = 0 }
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
			if m.activeTab == 0 { t1 = styleTabActive } 
			if m.activeTab == 1 { t2 = styleTabActive }
			if m.activeTab == 2 { t3 = styleTabActive }
			tabs = lipgloss.JoinHorizontal(lipgloss.Top, t1.Render("YAML"), t2.Render("Events"), t3.Render("Logs"))
		} else if curr.Type == "POD" {
			t1, t2 := styleTabInactive, styleTabInactive
			if m.activeTab == 0 { t1 = styleTabActive } else { t2 = styleTabActive }
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
		footer = styleCmdBar.Width(m.width).Render(m.textInput.View())
	} else {
		hint := " [:] Cmds  [/] Filter  [Tab] View  [Ctrl-F] Refresh  [rr] Restart  [s] Scale  [R] Rollback  [+] Add  [rm] Remove  [q] Quit"
		if m.activeFilter != "" {
			hint = fmt.Sprintf(" FILTER: \"%s\" (Esc to clear) | %s", m.activeFilter, hint)
		}
		footer = styleDim.Render(hint)
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, footer)
}

func runCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func executeCommand(input, helmRelease, deploymentName string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(input)
		if len(parts) == 0 { return nil }
		verb := parts[0]
		
		// :add is handled in Update now via addTargetMsg
		
		var cmd *exec.Cmd
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		switch verb {
		case "scale":
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: scale <replicas>")} }
			if deploymentName == "" { return detailsMsg{err: fmt.Errorf("No deployment selected")} }
			cmd = exec.CommandContext(ctx, "kubectl", "scale", "deployment", deploymentName, "--replicas="+parts[1], "-n", Namespace, "--context", Context)
		case "restart":
			if deploymentName == "" { return detailsMsg{err: fmt.Errorf("No deployment selected")} }
			cmd = exec.CommandContext(ctx, "kubectl", "rollout", "restart", "deployment", deploymentName, "-n", Namespace, "--context", Context)
		case "rollback":
			if helmRelease == "" { return detailsMsg{err: fmt.Errorf("No Helm release associated.")} }
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: rollback <revision>")} }
			cmd = exec.CommandContext(ctx, "helm", "rollback", helmRelease, parts[1], "-n", Namespace, "--kube-context", Context)
		case "fetch":
			return tea.Batch(
			    func() tea.Msg { return detailsMsg{content: "Manual Refresh...", isYaml: false} },
			    func() tea.Msg { return commandFinishedMsg{} },
			    tickCmd(),
			)()
		default:
			return detailsMsg{err: fmt.Errorf("Unknown command: %s", verb)}
		}
		out, err := cmd.CombinedOutput()
		if err != nil { return detailsMsg{err: fmt.Errorf("Failed:\n%s\n%s", err, string(out))} }
		return tea.Batch(
			func() tea.Msg { return detailsMsg{content: fmt.Sprintf("$ %s\n%s\nSUCCESS", input, string(out)), isYaml: false} },
			func() tea.Msg { return commandFinishedMsg{} },
			tickCmd(), 
		)()
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
				
				depOut, depErr := runCmd("kubectl", "get", "deployment", tName, "-n", Namespace, "--context", Context, "-o", "json")
				
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
				if val, ok := annotations["meta.helm.sh/release-name"]; ok { helmName = val.String() }
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
				for k := range selectorMap { keys = append(keys, k) }
				sort.Strings(keys)
				var labels []string
				for _, k := range keys { labels = append(labels, k+"="+selectorMap[k].String()) }
				newSelector := strings.Join(labels, ",")
				
				if newSelector != "" {
					mu.Lock()
					updatedSelectors[tName] = newSelector
					mu.Unlock()
					
					podOut, podErr := runCmd("kubectl", "get", "pods", "-n", Namespace, "--context", Context, "-l", newSelector, "-o", "json")
					if podErr == nil {
						gjson.Get(string(podOut), "items").ForEach(func(_, p gjson.Result) bool {
							phase := p.Get("status.phase").String()
							readyCount, totalCount := 0, 0
							p.Get("status.containerStatuses").ForEach(func(_, c gjson.Result) bool {
								totalCount++
								if c.Get("ready").Bool() { readyCount++ }
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
									if r := c.Get("state.waiting.reason").String(); r != "" { waitingReason = r; return false }
									return true
								})
								if waitingReason != "" { status = waitingReason }
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

func fetchDetailsCmd(i item, tab int, selectors map[string]string) tea.Cmd {
	return func() tea.Msg {
		var out []byte
		var err error
		isYaml := true
		
		if i.Type == "HDR" {
			return detailsMsg{content: "Service Group: " + i.Name, isYaml: false}
		}

		if i.Type == "DEP" {
			if tab == 1 { // Events
				out, err = runCmd("kubectl", "get", "events", "-n", Namespace, "--context", Context, "--sort-by=.lastTimestamp", "-o", "json")
				if err != nil { return detailsMsg{err: fmt.Errorf("%v: %s", err, string(out))} }
				var events []string
				events = append(events, fmt.Sprintf("%-25s %-10s %-15s %s", "TIMESTAMP", "TYPE", "REASON", "MESSAGE"))
				gjson.Get(string(out), "items").ForEach(func(_, e gjson.Result) bool {
					objName := e.Get("involvedObject.name").String()
					if strings.Contains(objName, i.Name) {
						ts := e.Get("lastTimestamp").String()
						if ts == "" { ts = e.Get("eventTime").String() }
						events = append(events, fmt.Sprintf("%-25s %-10s %-15s %s", ts, e.Get("type").String(), e.Get("reason").String(), e.Get("message").String()))
					}
					return true
				})
				if len(events) == 1 { return detailsMsg{content: "No recent events found.", isYaml: false} }
				return detailsMsg{content: strings.Join(events, "\n"), isYaml: false}
			} else if tab == 2 { // Aggregated Logs
				// Use cached selector data instead of kubectl call
				selector, exists := selectors[i.Name]
				if !exists || selector == "" {
					return detailsMsg{err: fmt.Errorf("No label selector found for deployment %s", i.Name)}
				}
				
				// Get logs from all pods using cached label selector
				out, err = runCmd("kubectl", "logs", "-l", selector, "-n", Namespace, "--context", Context, "--all-containers=true", "--prefix", "--tail=100")
				if err != nil { return detailsMsg{err: fmt.Errorf("Logs Err: %v", err)} }
				return detailsMsg{content: string(out), isYaml: false}
			}
		}

		if i.Type == "POD" && tab == 1 {
			out, err = runCmd("kubectl", "logs", i.Name, "-n", Namespace, "--context", Context, "--tail=200", "--all-containers=true")
			if err != nil { return detailsMsg{err: fmt.Errorf("%v: %s", err, string(out))} }
			return detailsMsg{content: string(out), isYaml: false}
		}

		if i.Type == "SEC" {
			out, err = runCmd("kubectl", "get", "secret", i.Name, "-n", Namespace, "--context", Context, "-o", "json")
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
			out, err = runCmd("helm", "history", i.Name, "-n", Namespace, "--kube-context", Context)
			isYaml = false
		} else {
			kind := "deployment"
			if i.Type == "POD" { kind = "pod" }
			if i.Type == "CM" { kind = "configmap" }
			out, err = runCmd("kubectl", "get", kind, i.Name, "-n", Namespace, "--context", Context, "-o", "yaml")
		}

		if err != nil { return detailsMsg{err: fmt.Errorf("%s\n%s", err.Error(), string(out))} }
		return detailsMsg{content: string(out), isYaml: isYaml}
	}
}

func highlight(content, format string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, format, "terminal256", "dracula")
	if err != nil { return content }
	return buf.String()
}

func getCurrentDeploymentName(items []item, cursor int) string {
	if len(items) == 0 || cursor >= len(items) { return "" }
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
	if deploymentName == "" { return "" }
	return helmReleases[deploymentName]
}

func isPositiveInteger(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" { return false }
	for _, r := range s {
		if r < '0' || r > '9' { return false }
	}
	return true
}

func isValidK8sName(name string) bool {
	if name == "" || len(name) > 253 {
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