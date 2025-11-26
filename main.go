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
	"github.com/alecthomas/chroma/v2/styles" // Ensure styles load
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
	
	styleTabActive   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(cPrimary).Foreground(cPrimary).Bold(true).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Padding(0, 1).Foreground(cGray)
	
	styleCmdBar = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("236")).Padding(0, 1)
	
	styleHighlight = lipgloss.NewStyle().Background(lipgloss.Color("201")).Foreground(lipgloss.Color("255")).Bold(true)
)

// Ensure styles are registered
func init() {
	_ = styles.Get("dracula")
}

// --- DATA MODEL ---
type item struct {
	Type   string // DEP, POD, HELM, SEC, CM, EVT
	Name   string
	Status string
}

type model struct {
	items       []item
	helmRelease string
	cachedSelector string 

	cursor      int
	listOffset  int
	listHeight  int

	activeTab    int 
	textInput    textinput.Model
	inputMode    bool
	filterMode   bool          
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
	items       []item
	helmRelease string
	selector    string
	err         error
}
type detailsMsg struct {
	content string
	isYaml  bool
	err     error
}
type commandFinishedMsg struct{}

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
	ti.Placeholder = "scale 3 | restart | rollback 1 | fetch"
	ti.Prompt = ": "
	ti.CharLimit = 156
	ti.Width = 50

	return model{
		textInput: ti,
		inputMode: false,
		listHeight: 20,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchDataCmd(""), tickCmd(), textinput.Blink)
}

// --- UPDATE ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// --- SYSTEM MESSAGES (Always Handle First) ---
	switch msg := msg.(type) {
	case tickMsg:
		// Keep the heartbeat alive
		return m, tea.Batch(fetchDataCmd(m.cachedSelector), tickCmd())

	case commandFinishedMsg:
		// Refresh immediately after a command finishes
		return m, fetchDataCmd(m.cachedSelector)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		headerHeight := 3 
		footerHeight := 1
		m.listHeight = msg.Height - headerHeight - footerHeight - 2
		if m.listHeight < 1 { m.listHeight = 1 }

		paneWidth := int(float64(msg.Width) * 0.35) // 35%
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
		// Continue to other handlers? No, window resize is standalone usually.
		return m, nil

	case dataMsg:
		m.lastUpd = time.Now()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.helmRelease = msg.helmRelease
			if msg.selector != "" {
				m.cachedSelector = msg.selector
			}
			
			oldLen := len(m.items)
			m.items = msg.items
			
			if len(m.items) > 0 && oldLen == 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[0], 0))
			}
			
			// Fix cursor drift
			if m.cursor >= len(m.items) { m.cursor = len(m.items) - 1 }
			if m.cursor < 0 { m.cursor = 0 }
			
			// Fix scroll drift
			if m.cursor < m.listOffset { m.listOffset = m.cursor }
			// This logic keeps the cursor in view if the list shrank
			if m.cursor >= m.listOffset + m.listHeight {
				m.listOffset = m.cursor - m.listHeight + 1
			}
			
			// Always refresh details for current selection to keep logs/events live
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
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
				} else {
					m.textInput.Reset()
					cmds = append(cmds, executeCommand(val, m.helmRelease))
				}
				return m, tea.Batch(cmds...)
				
			case "esc":
				m.inputMode = false
				m.filterMode = false
				m.textInput.Blur()
				m.textInput.Reset()
				return m, nil
			}
			
		default:
			if m.filterMode {
				m.textInput, cmd = m.textInput.Update(msg)
				m.activeFilter = m.textInput.Value()
				if m.activeFilter != "" {
					re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(m.activeFilter))
					if err == nil { m.filterRegex = re }
				} else {
					m.filterRegex = nil
				}
				m.updateViewportContent()
				return m, cmd
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
			m.textInput.Placeholder = "scale 3 | restart | rollback 1"
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
			cmds = append(cmds, fetchDataCmd(m.cachedSelector))

		case "1", "2", "3", "4", "5":
			target := ""
			switch msg.String() {
			case "1": target = "DEP"
			case "2": target = "HELM"
			case "3": target = "CM"
			case "4": target = "SEC"
			case "5": target = "POD"
			}
			
			nextIndex := -1
			startIndex := 0
			if len(m.items) > 0 && m.items[m.cursor].Type == target {
				startIndex = m.cursor + 1
			}
			for i := startIndex; i < len(m.items); i++ {
				if m.items[i].Type == target { nextIndex = i; break }
			}
			if nextIndex == -1 && startIndex > 0 {
				for i := 0; i < startIndex; i++ {
					if m.items[i].Type == target { nextIndex = i; break }
				}
			}
			
			if nextIndex != -1 {
				m.cursor = nextIndex
				if m.cursor < m.listOffset { m.listOffset = m.cursor }
				if m.cursor >= m.listOffset + m.listHeight {
					m.listOffset = m.cursor - m.listHeight + 1
				}
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.listOffset { m.listOffset = m.cursor }
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.listOffset + m.listHeight { m.listOffset++ }
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}

		case "tab":
			if len(m.items) > 0 {
				curr := m.items[m.cursor]
				if curr.Type == "DEP" || curr.Type == "POD" {
					if m.activeTab == 0 { m.activeTab = 1 } else { m.activeTab = 0 }
					cmds = append(cmds, fetchDetailsCmd(curr, m.activeTab))
				}
			}

		case "enter":
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}

		case "ctrl+l":
			if len(m.items) > 0 && m.items[m.cursor].Type == "POD" {
				m.activeTab = 1
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}
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

// --- VIEW ---
func (m model) View() string {
	if !m.ready { return "Initializing..." }

	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 20 { leftWidth = 20 }
	
	var listItems []string
	listItems = append(listItems, styleTitle.Render("🚀 "+Deployment))
	
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
			
			// Smart Truncation
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
			t1, t2 := styleTabInactive, styleTabInactive
			if m.activeTab == 0 { t1 = styleTabActive } else { t2 = styleTabActive }
			tabs = lipgloss.JoinHorizontal(lipgloss.Top, t1.Render("YAML"), t2.Render("Events"))
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
		hint := " [:] Cmds  [/] Filter  [Tab] View  [Ctrl-F] Refresh  [q] Quit"
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

func executeCommand(input, helmRelease string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(input)
		if len(parts) == 0 { return nil }
		verb := parts[0]
		var cmd *exec.Cmd
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		switch verb {
		case "scale":
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: scale <replicas>")} }
			cmd = exec.CommandContext(ctx, "kubectl", "scale", "deployment", Deployment, "--replicas="+parts[1], "-n", Namespace, "--context", Context)
		case "restart":
			cmd = exec.CommandContext(ctx, "kubectl", "rollout", "restart", "deployment", Deployment, "-n", Namespace, "--context", Context)
		case "rollback":
			if helmRelease == "" { return detailsMsg{err: fmt.Errorf("No Helm release associated.")} }
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: rollback <revision>")} }
			cmd = exec.CommandContext(ctx, "helm", "rollback", helmRelease, parts[1], "-n", Namespace, "--kube-context", Context)
		case "fetch":
			return tea.Batch(
			    func() tea.Msg { return detailsMsg{content: "Manual Refresh...", isYaml: false} },
			    func() tea.Msg { return commandFinishedMsg{} },
			    tickCmd(), // Restart tick loop if dead
			)()
		default:
			return detailsMsg{err: fmt.Errorf("Unknown command: %s", verb)}
		}
		out, err := cmd.CombinedOutput()
		if err != nil { return detailsMsg{err: fmt.Errorf("Failed:\\n%s\\n%s", err, string(out))} }
		return tea.Batch(
			func() tea.Msg { return detailsMsg{content: fmt.Sprintf("$ %s\\n%s\\nSUCCESS", input, string(out)), isYaml: false} },
			func() tea.Msg { return commandFinishedMsg{} },
			tickCmd(), // Ensure loop running
		)()
	}
}

func fetchDataCmd(cachedSelector string) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		var depOut, podOut []byte
		var depErr, podErr error
		
		wg.Add(1)
		go func() {
			defer wg.Done()
			depOut, depErr = runCmd("kubectl", "get", "deployment", Deployment, "-n", Namespace, "--context", Context, "-o", "json")
		}()
		
		if cachedSelector != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				podOut, podErr = runCmd("kubectl", "get", "pods", "-n", Namespace, "--context", Context, "-l", cachedSelector, "-o", "json")
			}()
		}
		
		wg.Wait()
		
		if depErr != nil {
			return dataMsg{err: fmt.Errorf("Dep Fetch: %v: %s", depErr, string(depOut))}
		}
		
		jsonRaw := string(depOut)
		seen := make(map[string]bool)
		var items []item
		addItem := func(t, n, s string) {
			key := t + ":" + n
			if !seen[key] && n != "" {
				seen[key] = true
				items = append(items, item{Type: t, Name: n, Status: s})
			}
		}

		addItem("DEP", Deployment, "Active")
		
		annotations := gjson.Get(jsonRaw, "metadata.annotations").Map()
		helmName := ""
		if val, ok := annotations["meta.helm.sh/release-name"]; ok { helmName = val.String() }
		if helmName == "" {
			labels := gjson.Get(jsonRaw, "metadata.labels").Map()
			if val, ok := labels["meta.helm.sh/release-name"]; ok { helmName = val.String() }
			if val, ok := labels["app.kubernetes.io/instance"]; ok && helmName == "" { helmName = val.String() }
		}
		if helmName != "" { addItem("HELM", helmName, "Release") }

		containers := gjson.Get(jsonRaw, "spec.template.spec.containers").Array()
		for _, c := range containers {
			c.Get("envFrom").ForEach(func(_, v gjson.Result) bool {
				if name := v.Get("secretRef.name").String(); name != "" { addItem("SEC", name, "Ref") }
				if name := v.Get("configMapRef.name").String(); name != "" { addItem("CM", name, "Ref") }
				return true
			})
			c.Get("env").ForEach(func(_, v gjson.Result) bool {
				if name := v.Get("valueFrom.secretKeyRef.name").String(); name != "" { addItem("SEC", name, "Ref") }
				if name := v.Get("valueFrom.configMapKeyRef.name").String(); name != "" { addItem("CM", name, "Ref") }
				return true
			})
		}
		gjson.Get(jsonRaw, "spec.template.spec.volumes").ForEach(func(_, v gjson.Result) bool {
			if name := v.Get("secret.secretName").String(); name != "" { addItem("SEC", name, "Vol") }
			if name := v.Get("configMap.name").String(); name != "" { addItem("CM", name, "Vol") }
			return true
		})

		newSelector := ""
		selectorMap := gjson.Get(jsonRaw, "spec.selector.matchLabels").Map()
		
		// Deterministic sort of selector keys
		keys := make([]string, 0, len(selectorMap))
		for k := range selectorMap { keys = append(keys, k) }
		sort.Strings(keys)
		
		var labels []string
		for _, k := range keys { labels = append(labels, k+"="+selectorMap[k].String()) }
		if len(labels) > 0 {
			newSelector = strings.Join(labels, ",")
		}
		
		if podOut == nil && newSelector != "" {
			podOut, podErr = runCmd("kubectl", "get", "pods", "-n", Namespace, "--context", Context, "-l", newSelector, "-o", "json")
		}
		
		if podErr == nil && podOut != nil {
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
					if waitingReason != "" { 
					    status = waitingReason 
					}
				}
				fullStatus := fmt.Sprintf("%s %d/%d", status, readyCount, totalCount)
				addItem("POD", p.Get("metadata.name").String(), fullStatus)
				return true
			})
		}

		return dataMsg{items: items, helmRelease: helmName, selector: newSelector, err: podErr}
	}
}

func fetchDetailsCmd(i item, tab int) tea.Cmd {
	return func() tea.Msg {
		var out []byte
		var err error
		isYaml := true

		if i.Type == "DEP" && tab == 1 {
			out, err = runCmd("kubectl", "get", "events", "-n", Namespace, "--context", Context, "--sort-by=.lastTimestamp", "-o", "json")
			if err != nil { return detailsMsg{err: fmt.Errorf("%v: %s", err, string(out))} }
			
			var events []string
			events = append(events, fmt.Sprintf("%-25s %-10s %-15s %s", "TIMESTAMP", "TYPE", "REASON", "MESSAGE"))
			
			gjson.Get(string(out), "items").ForEach(func(_, e gjson.Result) bool {
				objName := e.Get("involvedObject.name").String()
				if strings.Contains(objName, i.Name) {
					ts := e.Get("lastTimestamp").String()
					if ts == "" { ts = e.Get("eventTime").String() }
					line := fmt.Sprintf("%-25s %-10s %-15s %s", 
						ts, e.Get("type").String(), e.Get("reason").String(), e.Get("message").String())
					events = append(events, line)
				}
				return true
			})
			if len(events) == 1 {
				return detailsMsg{content: "No recent events found for this deployment.", isYaml: false}
			}
			return detailsMsg{content: strings.Join(events, "\\n"), isYaml: false}
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

		if err != nil { return detailsMsg{err: fmt.Errorf("%s\\n%s", err.Error(), string(out))} }
		return detailsMsg{content: string(out), isYaml: isYaml}
	}
}

func highlight(content, format string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, format, "terminal256", "dracula")
	if err != nil { return content }
	return buf.String()
}