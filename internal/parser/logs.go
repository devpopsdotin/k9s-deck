package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Constants for log processing
const (
	PodPrefixSuffixLen  = 7
	MaxPodPrefixDisplay = 20
	JSONIndent          = 2
	CommandTimeout      = 2 * time.Second
)

// Color palette
var (
	cRed    = lipgloss.Color("196") // Red
	cYellow = lipgloss.Color("220") // Yellow
	cGray   = lipgloss.Color("240") // Gray

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
)

// Regex patterns
var (
	logLevelRegex  = regexp.MustCompile(`(?i)\b(FATAL|ERROR|ERR|WARN|WARNING|INFO|DEBUG|TRACE)\b`)
	podPrefixRegex = regexp.MustCompile(`^\[([^/]+)/([^/]+)/([^\]]+)\]\s*(.*)$`)
)

// LogLineInfo contains parsed information from a log line
type LogLineInfo struct {
	OriginalLine  string
	PodPrefix     string // e.g., "nginx-deployment-5c7588df-abc123/nginx"
	PodName       string
	ContainerName string
	LogContent    string
	LogLevel      string // ERROR, WARN, INFO, DEBUG, etc.
	IsJSON        bool
}

// MultiContainerCache caches pod container information
type MultiContainerCache struct {
	mu    sync.RWMutex
	cache map[string]bool // podName -> hasMultipleContainers
}

// NewMultiContainerCache creates a new cache
func NewMultiContainerCache() *MultiContainerCache {
	return &MultiContainerCache{
		cache: make(map[string]bool),
	}
}

// ParseLogLine extracts components from a log line
func ParseLogLine(line string) LogLineInfo {
	info := LogLineInfo{
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

// GetPodColor returns a consistent color for a pod name using hash
func GetPodColor(podName string) lipgloss.Color {
	hash := 0
	for _, c := range podName {
		hash = (hash*31 + int(c)) % len(podColorPalette)
	}
	if hash < 0 {
		hash = -hash
	}
	return podColorPalette[hash%len(podColorPalette)]
}

// GetLogLevelColor returns the color for a log level
func GetLogLevelColor(level string) lipgloss.Color {
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

// ShortenPodPrefix extracts replicaset hash and pod suffix
func ShortenPodPrefix(podName, containerName string) string {
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

// FormatPodPrefix formats pod prefix with color and icon
func FormatPodPrefix(podName, containerName string) string {
	shortened := ShortenPodPrefix(podName, containerName)
	color := GetPodColor(podName)
	icon := "●"

	style := lipgloss.NewStyle().Foreground(color).Bold(true)
	return style.Render(icon + " " + shortened)
}

// ColorizeLogLevel applies color to log level keywords in a line
func ColorizeLogLevel(line string) string {
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
		color := GetLogLevelColor(level)
		style := lipgloss.NewStyle().Foreground(color).Bold(true)
		result.WriteString(style.Render(level))

		lastIndex = end
	}

	// Write remaining content
	result.WriteString(line[lastIndex:])
	return result.String()
}

// DetectJSONLog checks if a line is JSON format
func DetectJSONLog(line string) bool {
	trimmed := strings.TrimSpace(line)
	return (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))
}

// PrettyPrintJSONLog formats and highlights JSON logs
// Note: syntax highlighting should be applied by the caller using the syntax package
func PrettyPrintJSONLog(line string) string {
	// Try to parse and pretty-print JSON
	var obj interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return line // Return original if not valid JSON
	}

	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return line
	}

	return string(pretty)
}

// DetectMultiContainer checks if a pod has multiple containers (with caching)
// Note: This function requires kubectl and cluster context
func DetectMultiContainer(podName, namespace, kubeContext string, cache *MultiContainerCache) (bool, error) {
	// Check cache first
	cache.mu.RLock()
	if result, exists := cache.cache[podName]; exists {
		cache.mu.RUnlock()
		return result, nil
	}
	cache.mu.RUnlock()

	// Query kubectl
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", podName,
		"-n", namespace, "--context", kubeContext,
		"-o", "jsonpath={.spec.containers[*].name}")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, err
	}

	containerNames := strings.Fields(string(out))
	isMulti := len(containerNames) > 1

	// Cache result
	cache.mu.Lock()
	cache.cache[podName] = isMulti
	cache.mu.Unlock()

	return isMulti, nil
}

// ProcessLogContent is the master log processing function
// highlightFunc should be a function that applies syntax highlighting (e.g., from syntax package)
func ProcessLogContent(content, resourceType, resourceName string, formatMode bool, highlightFunc func(string, string) string) string {
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
		info := ParseLogLine(line)

		// Check if JSON
		if DetectJSONLog(info.LogContent) {
			// Format as JSON
			formatted := PrettyPrintJSONLog(info.LogContent)

			// Apply syntax highlighting if function provided
			if highlightFunc != nil {
				formatted = highlightFunc(formatted, "json")
			}

			if info.PodPrefix != "" {
				prefix := FormatPodPrefix(info.PodName, info.ContainerName)
				processed = append(processed, prefix+" "+formatted)
			} else {
				processed = append(processed, formatted)
			}
		} else {
			// Standard text log with level coloring
			formattedLine := line

			// Add pod prefix formatting if present
			if info.PodPrefix != "" {
				prefix := FormatPodPrefix(info.PodName, info.ContainerName)
				colorizedContent := ColorizeLogLevel(info.LogContent)
				formattedLine = prefix + " " + colorizedContent
			} else {
				formattedLine = ColorizeLogLevel(line)
			}

			processed = append(processed, formattedLine)
		}
	}

	return strings.Join(processed, "\n")
}
