package parser

import (
	"strings"
	"testing"
)

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantInfo LogLineInfo
	}{
		{
			name:  "log line with pod prefix",
			input: "[pod/nginx-abc123/nginx] ERROR: Connection failed",
			wantInfo: LogLineInfo{
				OriginalLine:  "[pod/nginx-abc123/nginx] ERROR: Connection failed",
				PodPrefix:     "nginx-abc123/nginx",
				PodName:       "nginx-abc123",
				ContainerName: "nginx",
				LogContent:    "ERROR: Connection failed",
				LogLevel:      "ERROR",
				IsJSON:        false,
			},
		},
		{
			name:  "log line without pod prefix",
			input: "INFO: Server started",
			wantInfo: LogLineInfo{
				OriginalLine:  "INFO: Server started",
				PodPrefix:     "",
				PodName:       "",
				ContainerName: "",
				LogContent:    "INFO: Server started",
				LogLevel:      "INFO",
				IsJSON:        false,
			},
		},
		{
			name:  "json log line",
			input: `{"level":"ERROR","message":"failed to connect"}`,
			wantInfo: LogLineInfo{
				OriginalLine:  `{"level":"ERROR","message":"failed to connect"}`,
				PodPrefix:     "",
				PodName:       "",
				ContainerName: "",
				LogContent:    `{"level":"ERROR","message":"failed to connect"}`,
				LogLevel:      "ERROR",
				IsJSON:        true,
			},
		},
		{
			name:  "log line with WARN level",
			input: "[pod/app-xyz789/app] WARN: High memory usage",
			wantInfo: LogLineInfo{
				OriginalLine:  "[pod/app-xyz789/app] WARN: High memory usage",
				PodPrefix:     "app-xyz789/app",
				PodName:       "app-xyz789",
				ContainerName: "app",
				LogContent:    "WARN: High memory usage",
				LogLevel:      "WARN",
				IsJSON:        false,
			},
		},
		{
			name:  "log line with DEBUG level",
			input: "DEBUG Connecting to database",
			wantInfo: LogLineInfo{
				OriginalLine:  "DEBUG Connecting to database",
				PodPrefix:     "",
				PodName:       "",
				ContainerName: "",
				LogContent:    "DEBUG Connecting to database",
				LogLevel:      "DEBUG",
				IsJSON:        false,
			},
		},
		{
			name:  "empty line",
			input: "",
			wantInfo: LogLineInfo{
				OriginalLine:  "",
				PodPrefix:     "",
				PodName:       "",
				ContainerName: "",
				LogContent:    "",
				LogLevel:      "",
				IsJSON:        false,
			},
		},
		{
			name:  "log line with FATAL level",
			input: "FATAL: Application crashed",
			wantInfo: LogLineInfo{
				OriginalLine:  "FATAL: Application crashed",
				PodPrefix:     "",
				PodName:       "",
				ContainerName: "",
				LogContent:    "FATAL: Application crashed",
				LogLevel:      "FATAL",
				IsJSON:        false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLogLine(tt.input)

			if got.OriginalLine != tt.wantInfo.OriginalLine {
				t.Errorf("OriginalLine = %q, want %q", got.OriginalLine, tt.wantInfo.OriginalLine)
			}
			if got.PodPrefix != tt.wantInfo.PodPrefix {
				t.Errorf("PodPrefix = %q, want %q", got.PodPrefix, tt.wantInfo.PodPrefix)
			}
			if got.PodName != tt.wantInfo.PodName {
				t.Errorf("PodName = %q, want %q", got.PodName, tt.wantInfo.PodName)
			}
			if got.ContainerName != tt.wantInfo.ContainerName {
				t.Errorf("ContainerName = %q, want %q", got.ContainerName, tt.wantInfo.ContainerName)
			}
			if got.LogContent != tt.wantInfo.LogContent {
				t.Errorf("LogContent = %q, want %q", got.LogContent, tt.wantInfo.LogContent)
			}
			if got.LogLevel != tt.wantInfo.LogLevel {
				t.Errorf("LogLevel = %q, want %q", got.LogLevel, tt.wantInfo.LogLevel)
			}
			if got.IsJSON != tt.wantInfo.IsJSON {
				t.Errorf("IsJSON = %v, want %v", got.IsJSON, tt.wantInfo.IsJSON)
			}
		})
	}
}

func TestDetectJSONLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid json object", `{"key":"value"}`, true},
		{"valid json array", `[1,2,3]`, true},
		{"json with whitespace", `  {"key":"value"}  `, true},
		{"plain text", "ERROR: Something went wrong", false},
		{"partial json", `{"incomplete`, false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectJSONLog(tt.input)
			if got != tt.want {
				t.Errorf("DetectJSONLog(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestShortenPodPrefix(t *testing.T) {
	tests := []struct {
		name          string
		podName       string
		containerName string
		want          string
	}{
		{
			name:          "standard pod name",
			podName:       "nginx-deployment-55c74d7f8-zn5fd",
			containerName: "nginx",
			want:          "[55c74d7f8-zn5fd]",
		},
		{
			name:          "long deployment name",
			podName:       "my-awesome-service-deployment-abc123-xyz789",
			containerName: "app",
			want:          "[abc123-xyz789]",
		},
		{
			name:          "short pod name",
			podName:       "pod-abc",
			containerName: "container",
			want:          "[pod-abc]",
		},
		{
			name:          "single part name",
			podName:       "standalone",
			containerName: "app",
			want:          "[standalone]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShortenPodPrefix(tt.podName, tt.containerName)
			if got != tt.want {
				t.Errorf("ShortenPodPrefix(%q, %q) = %q, want %q",
					tt.podName, tt.containerName, got, tt.want)
			}
		})
	}
}

func TestPrettyPrintJSONLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "compact json",
			input: `{"level":"ERROR","message":"failed"}`,
			want: `{
  "level": "ERROR",
  "message": "failed"
}`,
		},
		{
			name:  "invalid json",
			input: `{invalid`,
			want:  `{invalid`,
		},
		{
			name:  "json array",
			input: `[1,2,3]`,
			want: `[
  1,
  2,
  3
]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrettyPrintJSONLog(tt.input)
			if got != tt.want {
				t.Errorf("PrettyPrintJSONLog() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProcessLogContent(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		resourceType string
		resourceName string
		formatMode   bool
		wantContains []string
	}{
		{
			name:         "raw mode returns unchanged",
			content:      "ERROR: test log",
			resourceType: "POD",
			resourceName: "test-pod",
			formatMode:   false,
			wantContains: []string{"ERROR: test log"},
		},
		{
			name:         "formatted mode with log level",
			content:      "INFO: Server started\nERROR: Connection failed",
			resourceType: "POD",
			resourceName: "test-pod",
			formatMode:   true,
			wantContains: []string{"INFO", "ERROR"},
		},
		{
			name:         "empty content",
			content:      "",
			resourceType: "POD",
			resourceName: "test-pod",
			formatMode:   true,
			wantContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProcessLogContent(tt.content, tt.resourceType, tt.resourceName, tt.formatMode, nil)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("ProcessLogContent() result should contain %q, got %q", want, got)
				}
			}
		})
	}
}

func TestGetPodColor(t *testing.T) {
	// Test that same pod name always gets same color
	pod1 := "nginx-abc123"
	color1 := GetPodColor(pod1)
	color2 := GetPodColor(pod1)

	if color1 != color2 {
		t.Errorf("GetPodColor should return consistent color for same pod, got %v and %v", color1, color2)
	}

	// Test that different pods get valid colors from palette
	pod2 := "app-xyz789"
	color3 := GetPodColor(pod2)

	// Verify it's from the palette
	found := false
	for _, c := range podColorPalette {
		if c == color3 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetPodColor should return color from palette, got %v", color3)
	}
}

func TestGetLogLevelColor(t *testing.T) {
	tests := []struct {
		level string
		want  string // We'll just check it returns a non-empty color
	}{
		{"ERROR", string(cRed)},
		{"error", string(cRed)}, // Case insensitive
		{"WARN", string(cYellow)},
		{"WARNING", string(cYellow)},
		{"INFO", "39"},
		{"DEBUG", string(cGray)},
		{"UNKNOWN", "255"},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			got := GetLogLevelColor(tt.level)
			if string(got) != tt.want {
				t.Errorf("GetLogLevelColor(%q) = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}
