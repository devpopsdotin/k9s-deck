package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/devpopsdotin/k9s-deck/internal/k8s"
)

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

func TestClampListOffset(t *testing.T) {
	// List shrank from 30 to 8 items while scrolled down
	m := model{items: make([]item, 8), cursor: 7, listOffset: 20, listHeight: 5}
	m.clampListOffset()
	if m.listOffset != 3 {
		t.Errorf("listOffset = %d, want 3", m.listOffset)
	}

	// Everything fits on screen
	m = model{items: make([]item, 4), cursor: 2, listOffset: 2, listHeight: 10}
	m.clampListOffset()
	if m.listOffset != 0 {
		t.Errorf("listOffset = %d, want 0", m.listOffset)
	}
}

func TestFetchDataCmdBuildsItemsFromClient(t *testing.T) {
	mock := k8s.NewMockClient()
	mock.GetDeploymentFunc = func(_ context.Context, ns, name string) ([]byte, error) {
		if ns != "prod" {
			t.Errorf("GetDeployment namespace = %q, want prod", ns)
		}
		if name == "missing" {
			return nil, errors.New("not found")
		}
		return []byte(`{
			"metadata": {"annotations": {"meta.helm.sh/release-name": "web-release"}},
			"spec": {
				"selector": {"matchLabels": {"tier": "frontend", "app": "web"}},
				"template": {"spec": {
					"containers": [{"envFrom": [{"secretRef": {"name": "web-secret"}}]}],
					"volumes": [{"configMap": {"name": "web-config"}}]
				}}
			}
		}`), nil
	}
	mock.ListPodsFunc = func(_ context.Context, _, selector string) ([]byte, error) {
		if selector != "app=web,tier=frontend" {
			t.Errorf("ListPods selector = %q, want sorted labels", selector)
		}
		return []byte(`{"items": [{
			"metadata": {"name": "web-5c7588df9-zn5fd"},
			"status": {"phase": "Running", "containerStatuses": [{"ready": true}]}
		}]}`), nil
	}

	cl := &cluster{client: mock, namespace: "prod"}
	msg, ok := cl.fetchDataCmd([]string{"web", "missing"}, 7)().(dataMsg)
	if !ok {
		t.Fatal("fetchDataCmd did not return a dataMsg")
	}

	if msg.seq != 7 {
		t.Errorf("seq = %d, want 7", msg.seq)
	}
	if msg.err == nil || !strings.Contains(msg.err.Error(), "missing") {
		t.Errorf("err = %v, want an error naming the missing target", msg.err)
	}

	want := []item{
		{Type: "HDR", Name: "=== missing (Err) ==="},
		{Type: "HDR", Name: "=== web ==="},
		{Type: "DEP", Name: "web", Status: "Active"},
		{Type: "HELM", Name: "web-release", Status: "Release"},
		{Type: "SEC", Name: "web-secret", Status: "Ref"},
		{Type: "CM", Name: "web-config", Status: "Ref"},
		{Type: "POD", Name: "web-5c7588df9-zn5fd", Status: "Running 1/1"},
	}
	if len(msg.items) != len(want) {
		t.Fatalf("got %d items %+v, want %d", len(msg.items), msg.items, len(want))
	}
	for i := range want {
		if msg.items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, msg.items[i], want[i])
		}
	}
	if msg.selectors["web"] != "app=web,tier=frontend" {
		t.Errorf("selector = %q", msg.selectors["web"])
	}
	if msg.helmReleases["web"] != "web-release" {
		t.Errorf("helm release = %q", msg.helmReleases["web"])
	}
}

func TestModelRendersFetchedItems(t *testing.T) {
	mock := k8s.NewMockClient()
	mock.GetDeploymentFunc = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"spec": {"selector": {"matchLabels": {"app": "web"}}}}`), nil
	}
	mock.ListPodsFunc = func(_ context.Context, _, _ string) ([]byte, error) {
		return []byte(`{"items": [{"metadata": {"name": "web-5c7588df9-zn5fd"},
			"status": {"phase": "Running", "containerStatuses": [{"ready": true}]}}]}`), nil
	}

	m := initialModel(Config{Client: mock, Context: "test-ctx", Namespace: "default", Deployment: "web"})
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	tm, _ = tm.Update(m.cluster.fetchDataCmd(m.targets, m.fetchSeq)())

	view := tm.View()
	for _, want := range []string{"K9s Deck", "test-ctx", "=== web ===", "web-5c7588df9-zn5fd", "Running 1/1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}
