package ui

import "time"

// Item represents a resource in the list (Deployment, Pod, Helm, Secret, ConfigMap, Header)
type Item struct {
	Type   string // DEP, POD, HELM, SEC, CM, HDR
	Name   string
	Status string
}

// Bubble Tea messages

// TickMsg is sent periodically to trigger UI updates
type TickMsg time.Time

// DataMsg contains fetched deployment data
type DataMsg struct {
	Items        []Item
	Selectors    map[string]string
	HelmReleases map[string]string
	Err          error
}

// DetailsMsg contains details for the selected resource
type DetailsMsg struct {
	Content string
	IsYaml  bool
	Err     error
}

// CommandFinishedMsg indicates a command has completed
type CommandFinishedMsg struct{}

// AddTargetMsg requests adding a deployment to monitor
type AddTargetMsg struct {
	Name string
}

// RemoveTargetMsg requests removing a deployment from monitoring
type RemoveTargetMsg struct {
	Name string
}

// SuggestionsMsg contains autocomplete suggestions for deployments
type SuggestionsMsg struct {
	Deployments []string
}

// CopyMsg indicates clipboard copy result
type CopyMsg struct {
	Success bool
	Err     error
}

// ClearStatusMsg requests clearing the status message
type ClearStatusMsg struct{}
