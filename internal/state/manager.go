package state

import "sync"

// Manager provides thread-safe access to deployment state
type Manager struct {
	mu           sync.RWMutex
	selectors    map[string]string // deployment name -> label selector
	helmReleases map[string]string // deployment name -> helm release name
}

// NewManager creates a new state manager
func NewManager() *Manager {
	return &Manager{
		selectors:    make(map[string]string),
		helmReleases: make(map[string]string),
	}
}

// GetSelector returns the label selector for a deployment (thread-safe read)
func (m *Manager) GetSelector(deployment string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	selector, ok := m.selectors[deployment]
	return selector, ok
}

// GetAllSelectors returns a copy of all selectors (thread-safe)
func (m *Manager) GetAllSelectors() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make(map[string]string, len(m.selectors))
	for k, v := range m.selectors {
		copied[k] = v
	}
	return copied
}

// SetSelector sets the label selector for a deployment (thread-safe write)
func (m *Manager) SetSelector(deployment, selector string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectors[deployment] = selector
}

// MergeSelectors merges multiple selectors (thread-safe bulk write)
func (m *Manager) MergeSelectors(updates map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range updates {
		m.selectors[k] = v
	}
}

// GetHelmRelease returns the helm release name for a deployment (thread-safe read)
func (m *Manager) GetHelmRelease(deployment string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	release, ok := m.helmReleases[deployment]
	return release, ok
}

// GetAllHelmReleases returns a copy of all helm releases (thread-safe)
func (m *Manager) GetAllHelmReleases() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	copied := make(map[string]string, len(m.helmReleases))
	for k, v := range m.helmReleases {
		copied[k] = v
	}
	return copied
}

// SetHelmRelease sets the helm release for a deployment (thread-safe write)
func (m *Manager) SetHelmRelease(deployment, release string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.helmReleases[deployment] = release
}

// MergeHelmReleases merges multiple helm releases (thread-safe bulk write)
func (m *Manager) MergeHelmReleases(updates map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range updates {
		m.helmReleases[k] = v
	}
}

// DeleteDeployment removes all state for a deployment (thread-safe)
func (m *Manager) DeleteDeployment(deployment string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.selectors, deployment)
	delete(m.helmReleases, deployment)
}

// Clear removes all state (thread-safe)
func (m *Manager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selectors = make(map[string]string)
	m.helmReleases = make(map[string]string)
}
