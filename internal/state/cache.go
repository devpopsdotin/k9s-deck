package state

import "sync"

// MultiContainerCache caches whether pods have multiple containers
type MultiContainerCache struct {
	mu    sync.RWMutex
	cache map[string]bool // podName -> hasMultipleContainers
}

// NewMultiContainerCache creates a new multi-container cache
func NewMultiContainerCache() *MultiContainerCache {
	return &MultiContainerCache{
		cache: make(map[string]bool),
	}
}

// Get returns whether a pod has multiple containers (thread-safe read)
func (c *MultiContainerCache) Get(podName string) (bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, exists := c.cache[podName]
	return result, exists
}

// Set caches the multi-container status for a pod (thread-safe write)
func (c *MultiContainerCache) Set(podName string, hasMultipleContainers bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[podName] = hasMultipleContainers
}

// Clear removes all cached entries (thread-safe)
func (c *MultiContainerCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]bool)
}

// Size returns the number of cached entries (thread-safe)
func (c *MultiContainerCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}
