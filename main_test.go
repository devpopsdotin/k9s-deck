package main

import "testing"

func TestIsDeploymentEvent(t *testing.T) {
	tests := []struct {
		kind, obj string
		want      bool
	}{
		{"Deployment", "api", true},
		{"ReplicaSet", "api-5c7588df9", true},
		{"Pod", "api-5c7588df9-zn5fd", true},
		// Other deployments sharing the "api" prefix
		{"Deployment", "api-gateway", false},
		{"ReplicaSet", "api-gateway-5c7588df9", false},
		{"Pod", "api-gateway-5c7588df9-zn5fd", false},
		// Unrelated kinds
		{"Service", "api", false},
	}
	for _, tt := range tests {
		if got := isDeploymentEvent(tt.kind, tt.obj, "api"); got != tt.want {
			t.Errorf("isDeploymentEvent(%q, %q, \"api\") = %v, want %v", tt.kind, tt.obj, got, tt.want)
		}
	}
}

func TestGetFilteredSuggestionsKeepsSelectionVisible(t *testing.T) {
	m := model{
		showSuggestions: true,
		suggestions:     []string{"a", "b", "c", "d", "e", "f", "g"},
	}
	for idx := range m.suggestions {
		m.suggestionIndex = idx
		visible, offset := m.getFilteredSuggestions()
		if len(visible) != MaxSuggestions {
			t.Fatalf("index %d: got %d visible, want %d", idx, len(visible), MaxSuggestions)
		}
		if idx < offset || idx >= offset+len(visible) {
			t.Errorf("index %d not visible in window starting at %d", idx, offset)
		}
		if visible[idx-offset] != m.suggestions[idx] {
			t.Errorf("index %d: highlighted %q, want %q", idx, visible[idx-offset], m.suggestions[idx])
		}
	}
}
