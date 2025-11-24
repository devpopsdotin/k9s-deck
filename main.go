package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/quick"
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
)

// --- DATA MODEL ---
type item struct {
	Type   string // DEP, POD, HELM, SEC, CM, EVT
	Name   string
	Status string
}

type model struct {
	items       []item
	helmRelease string
	cursor      int
	
	listOffset int
	listHeight int

	activeTab   int 
	textInput   textinput.Model
	inputMode   bool
	viewport    viewport.Model
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
	err         error
}
type detailsMsg struct {
	content string
	isYaml  bool
	err     error
}
// Signal that a command finished and we should refresh immediately
type commandFinishedMsg struct{}

// --- MAIN ---
func main() {
	if len(os.Args) < 4 {
		if os.Getenv("KUBECONFIG") != "" {
			Context = "kind-kind"
			Namespace = "default"
			Deployment = "hello-app"
		} else {
			fmt.Println("Usage: k9s-lens <context> <namespace> <deployment>")
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
	return tea.Batch(fetchDataCmd(), tickCmd(), textinput.Blink)
}

// --- UPDATE ---
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	// 1. Command Mode
	if m.inputMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				cmdText := m.textInput.Value()
				m.inputMode = false
				m.textInput.Blur()
				m.textInput.Reset()
				// Execute command AND trigger immediate refresh
				cmds = append(cmds, executeCommand(cmdText, m.helmRelease))
				return m, tea.Batch(cmds...)
			case "esc":
				m.inputMode = false
				m.textInput.Blur()
				m.textInput.Reset()
				return m, nil
			}
		}
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	// 2. Normal Mode
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		headerHeight := 3 
		footerHeight := 1
		m.listHeight = msg.Height - headerHeight - footerHeight - 2
		if m.listHeight < 1 { m.listHeight = 1 }

		paneWidth := int(float64(msg.Width) * 0.40) // 40%
		vpWidth := msg.Width - paneWidth - 4
		vpHeight := msg.Height - headerHeight - footerHeight - 2 
		
		if !m.ready {
			m.viewport = viewport.New(vpWidth, vpHeight)
			m.viewport.YPosition = headerHeight + 1
			m.ready = true
		} else {
			m.viewport.Width = vpWidth
			m.viewport.Height = vpHeight
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case ":":
			m.inputMode = true
			m.textInput.Focus()
			return m, textinput.Blink
			
		// --- MANUAL REFRESH ---
		case "ctrl+f":
			cmds = append(cmds, fetchDataCmd())

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.listOffset {
					m.listOffset = m.cursor
				}
				m.activeTab = 0
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				if m.cursor >= m.listOffset + m.listHeight {
					m.listOffset++
				}
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

		case "ctrl+s":
			if len(m.items) > 0 && m.items[m.cursor].Type == "POD" {
				return m, openLessCmd(m.items[m.cursor].Name)
			}
		}

	case tickMsg:
		return m, tea.Batch(fetchDataCmd(), tickCmd())

	// Triggered immediately after a command (scale/restart)
	case commandFinishedMsg:
		return m, fetchDataCmd()

	case dataMsg:
		m.lastUpd = time.Now()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil // Clear error on success
			m.helmRelease = msg.helmRelease
			oldLen := len(m.items)
			m.items = msg.items
			
			if len(m.items) > 0 && oldLen == 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[0], 0))
			}
			
			// Fix Selection Bug: Clamp cursor AND Offset
			if m.cursor >= len(m.items) {
				m.cursor = len(m.items) - 1
			}
			if m.cursor < 0 { m.cursor = 0 }
			
			// If cursor moves above offset (due to shrink), fix offset
			if m.cursor < m.listOffset {
				m.listOffset = m.cursor
			}
			
			// Only refresh details if we have items
			if len(m.items) > 0 {
				cmds = append(cmds, fetchDetailsCmd(m.items[m.cursor], m.activeTab))
			}
		}

	case detailsMsg:
		if msg.err != nil {
			m.viewport.SetContent(styleErr.Render(fmt.Sprintf("Error: %v", msg.err)))
		} else {
			content := msg.content
			if msg.isYaml {
				content = highlight(content, "yaml")
			}
			// Safe wrapping width
			wrapWidth := m.viewport.Width - 2
			if wrapWidth < 10 { wrapWidth = 10 }
			wrapper := lipgloss.NewStyle().Width(wrapWidth) 
			m.viewport.SetContent(wrapper.Render(content))
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// --- VIEW ---
func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// 55% Width for List to accommodate long pod names
	leftWidth := int(float64(m.width) * 0.40)
	if leftWidth < 20 { leftWidth = 20 }
	
	// 1. LEFT PANE - Stacked Vertically
	var listItems []string
	
	// Header
	listItems = append(listItems, styleTitle.Render("🚀 "+Deployment))
	if m.err != nil {
		listItems = append(listItems, styleErr.Render("Err: " + m.err.Error()))
	} else {
		listItems = append(listItems, styleDim.Render(m.lastUpd.Format("15:04:05")))
	}
	listItems = append(listItems, "") // Spacer

	if len(m.items) == 0 {
		listItems = append(listItems, "Loading resources...")
	} else {
		end := m.listOffset + m.listHeight
		if end > len(m.items) {
			end = len(m.items)
		}

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
			
			// SMART TRUNCATION
			// Layout: Icon(2) + Type(4) + Name(?) + Status(?)
			// We prioritize Status.
			// Fixed width parts: Icon(2 chars) + Spaces(3) + Type(4) = ~9 chars
			
			availNameWidth := leftWidth - 9 - len(statusStr) - 2
			
			// CRITICAL FIX: Prevent slicing panic if width is too small
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

	// 2. RIGHT PANE
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
	
	// 3. MAIN LAYOUT
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightStack)

	// 4. FOOTER
	var footer string
	if m.inputMode {
		footer = styleCmdBar.Width(m.width).Render(m.textInput.View())
	} else {
		footer = styleDim.Render(" [:] Cmds  [↑/↓] Select  [Enter] Detail  [Ctrl-F] Force Refresh  [Ctrl-S] Logs  [q] Quit")
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, footer)
}

// --- COMMANDS ---

// Helper to run command with timeout and return combined output (stderr included)
func runCmd(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput() // Changed to CombinedOutput so we see errors in UI
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) // Longer timeout for actions
		defer cancel()

		switch verb {
		case "scale":
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: scale <replicas>")} }
			cmd = exec.CommandContext(ctx, "kubectl", "scale", "deployment", Deployment, "--replicas="+parts[1], "-n", Namespace, "--context", Context)
		case "restart":
			cmd = exec.CommandContext(ctx, "kubectl", "rollout", "restart", "deployment", Deployment, "-n", Namespace, "--context", Context)
		case "rollback":
			if helmRelease == "" { return detailsMsg{err: fmt.Errorf("No Helm release associated with this deployment.")} }
			if len(parts) < 2 { return detailsMsg{err: fmt.Errorf("Usage: rollback <revision>")} }
			cmd = exec.CommandContext(ctx, "helm", "rollback", helmRelease, parts[1], "-n", Namespace, "--kube-context", Context)
		case "fetch":
		    // Manual fetch trigger
			return tea.Batch(
			    func() tea.Msg { return detailsMsg{content: fmt.Sprintf("$ %s\\nManual Refresh Triggered", input), isYaml: false} },
			    func() tea.Msg { return commandFinishedMsg{} },
			)()
		default:
			return detailsMsg{err: fmt.Errorf("Unknown command: %s\\nAvailable: scale, restart, rollback, fetch", verb)}
		}

		out, err := cmd.CombinedOutput()
		outputStr := string(out)
		if err != nil {
			return detailsMsg{err: fmt.Errorf("Command Failed:\\n%s\\n%s", err, outputStr)}
		}
		
		return tea.Batch(
			func() tea.Msg { return detailsMsg{content: fmt.Sprintf("$ %s\\n%s\\nSUCCESS", input, outputStr), isYaml: false} },
			func() tea.Msg { return commandFinishedMsg{} },
		)()
	}
}

func fetchDataCmd() tea.Cmd {
	return func() tea.Msg {
		// Use runCmd with timeout
		out, err := runCmd("kubectl", "get", "deployment", Deployment, "-n", Namespace, "--context", Context, "-o", "json")
		if err != nil { 
		    // If runCmd failed, out contains the stderr which explains WHY
		    return dataMsg{err: fmt.Errorf("%v: %s", err, string(out))} 
		}
		jsonRaw := string(out)
		
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

		// HELM Detection
		annotations := gjson.Get(jsonRaw, "metadata.annotations").Map()
		helmName := ""
		if val, ok := annotations["meta.helm.sh/release-name"]; ok {
			helmName = val.String()
		}
		if helmName == "" {
			labels := gjson.Get(jsonRaw, "metadata.labels").Map()
			if val, ok := labels["meta.helm.sh/release-name"]; ok {
				helmName = val.String()
			}
			if val, ok := labels["app.kubernetes.io/instance"]; ok && helmName == "" {
				helmName = val.String()
			}
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

		selectorMap := gjson.Get(jsonRaw, "spec.selector.matchLabels").Map()
		var labels []string
		for k, v := range selectorMap { labels = append(labels, k+"="+v.String()) }
		if len(labels) > 0 {
			// Fetch pods with timeout
			podOut, err := runCmd("kubectl", "get", "pods", "-n", Namespace, "--context", Context, "-l", strings.Join(labels, ","), "-o", "json")
			if err != nil { return dataMsg{err: fmt.Errorf("%v: %s", err, string(podOut))} }
			
			gjson.Get(string(podOut), "items").ForEach(func(_, p gjson.Result) bool {
				phase := p.Get("status.phase").String()
				
				readyCount := 0
				totalCount := 0
				p.Get("status.containerStatuses").ForEach(func(_, c gjson.Result) bool {
					totalCount++
					if c.Get("ready").Bool() { readyCount++ }
					return true
				})
				
				// CRITICAL FIX: If pod is Ready (1/1), assume Running.
				// This ignores ephemeral "waiting" states in history.
				isReady := totalCount > 0 && readyCount == totalCount
				
				status := phase
				if p.Get("metadata.deletionTimestamp").Exists() {
					status = "Terminating"
				} else if isReady {
					status = "Running"
				} else {
					// Only check for errors if NOT ready
					waitingReason := ""
					p.Get("status.containerStatuses").ForEach(func(_, c gjson.Result) bool {
						if r := c.Get("state.waiting.reason").String(); r != "" {
							waitingReason = r
							return false
						}
						if r := c.Get("state.terminated.reason").String(); r != "" && r != "Completed" {
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
				addItem("POD", p.Get("metadata.name").String(), fullStatus)
				return true
			})
		}
		return dataMsg{items: items, helmRelease: helmName}
	}
}

func fetchDetailsCmd(i item, tab int) tea.Cmd {
	return func() tea.Msg {
		var out []byte
		var err error
		isYaml := true

		if i.Type == "DEP" && tab == 1 {
			// Use pure kubectl to get events, then filter in Go to avoid shell dependency/grep
			out, err = runCmd("kubectl", "get", "events", "-n", Namespace, "--context", Context, "--sort-by=.lastTimestamp")
			if err != nil { 
				return detailsMsg{err: fmt.Errorf("%v: %s", err, string(out))}
			}
			
			// Simple line-based filtering
			lines := strings.Split(string(out), "\\n")
			var filtered []string
			header := ""
			if len(lines) > 0 { header = lines[0] }
			filtered = append(filtered, header)
			
			count := 0
			for _, line := range lines {
				if strings.Contains(line, i.Name) {
					filtered = append(filtered, line)
					count++
				}
			}
			if count == 0 {
				return detailsMsg{content: "No recent events found.", isYaml: false}
			}
			return detailsMsg{content: strings.Join(filtered, "\\n"), isYaml: false}
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
			out, err = exec.Command("helm", "history", i.Name, "-n", Namespace, "--kube-context", Context).CombinedOutput()
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

func openLessCmd(podName string) tea.Cmd {
	return tea.ExecProcess(exec.Command("sh", "-c",
		fmt.Sprintf("kubectl logs %s -n %s --context %s --tail=5000 | less +G", podName, Namespace, Context)),
		func(err error) tea.Msg { return nil })
}

func highlight(content, format string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, format, "terminal256", "dracula")
	if err != nil { return content }
	return buf.String()
}