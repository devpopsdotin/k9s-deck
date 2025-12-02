package state

import (
	"sync"
	"testing"
)

func TestManager_GetSetSelector(t *testing.T) {
	m := NewManager()

	// Test setting and getting a selector
	m.SetSelector("deploy1", "app=test")

	selector, ok := m.GetSelector("deploy1")
	if !ok {
		t.Error("Expected to find selector, got not found")
	}
	if selector != "app=test" {
		t.Errorf("Expected 'app=test', got '%s'", selector)
	}

	// Test getting non-existent selector
	_, ok = m.GetSelector("nonexistent")
	if ok {
		t.Error("Expected not to find selector, but found one")
	}
}

func TestManager_MergeSelectors(t *testing.T) {
	m := NewManager()

	updates := map[string]string{
		"deploy1": "app=test1",
		"deploy2": "app=test2",
		"deploy3": "app=test3",
	}

	m.MergeSelectors(updates)

	for name, expected := range updates {
		selector, ok := m.GetSelector(name)
		if !ok {
			t.Errorf("Expected to find selector for %s", name)
		}
		if selector != expected {
			t.Errorf("Expected '%s', got '%s'", expected, selector)
		}
	}
}

func TestManager_GetSetHelmRelease(t *testing.T) {
	m := NewManager()

	// Test setting and getting a helm release
	m.SetHelmRelease("deploy1", "release1")

	release, ok := m.GetHelmRelease("deploy1")
	if !ok {
		t.Error("Expected to find helm release, got not found")
	}
	if release != "release1" {
		t.Errorf("Expected 'release1', got '%s'", release)
	}

	// Test getting non-existent release
	_, ok = m.GetHelmRelease("nonexistent")
	if ok {
		t.Error("Expected not to find helm release, but found one")
	}
}

func TestManager_MergeHelmReleases(t *testing.T) {
	m := NewManager()

	updates := map[string]string{
		"deploy1": "release1",
		"deploy2": "release2",
	}

	m.MergeHelmReleases(updates)

	for name, expected := range updates {
		release, ok := m.GetHelmRelease(name)
		if !ok {
			t.Errorf("Expected to find helm release for %s", name)
		}
		if release != expected {
			t.Errorf("Expected '%s', got '%s'", expected, release)
		}
	}
}

func TestManager_DeleteDeployment(t *testing.T) {
	m := NewManager()

	// Setup
	m.SetSelector("deploy1", "app=test")
	m.SetHelmRelease("deploy1", "release1")

	// Delete
	m.DeleteDeployment("deploy1")

	// Verify both are deleted
	_, ok := m.GetSelector("deploy1")
	if ok {
		t.Error("Expected selector to be deleted")
	}

	_, ok = m.GetHelmRelease("deploy1")
	if ok {
		t.Error("Expected helm release to be deleted")
	}
}

func TestManager_Clear(t *testing.T) {
	m := NewManager()

	// Setup
	m.SetSelector("deploy1", "app=test1")
	m.SetSelector("deploy2", "app=test2")
	m.SetHelmRelease("deploy1", "release1")

	// Clear
	m.Clear()

	// Verify all cleared
	selectors := m.GetAllSelectors()
	if len(selectors) != 0 {
		t.Errorf("Expected 0 selectors after clear, got %d", len(selectors))
	}

	releases := m.GetAllHelmReleases()
	if len(releases) != 0 {
		t.Errorf("Expected 0 helm releases after clear, got %d", len(releases))
	}
}

func TestManager_GetAllSelectors(t *testing.T) {
	m := NewManager()

	expected := map[string]string{
		"deploy1": "app=test1",
		"deploy2": "app=test2",
	}

	m.MergeSelectors(expected)

	all := m.GetAllSelectors()

	if len(all) != len(expected) {
		t.Errorf("Expected %d selectors, got %d", len(expected), len(all))
	}

	for k, v := range expected {
		if all[k] != v {
			t.Errorf("Expected selector '%s' for '%s', got '%s'", v, k, all[k])
		}
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			deployment := "deploy"
			selector := "app=test"
			m.SetSelector(deployment, selector)
			m.SetHelmRelease(deployment, "release")
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.GetSelector("deploy")
			m.GetHelmRelease("deploy")
			m.GetAllSelectors()
			m.GetAllHelmReleases()
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	selector, ok := m.GetSelector("deploy")
	if !ok {
		t.Error("Expected to find selector after concurrent access")
	}
	if selector != "app=test" {
		t.Errorf("Expected 'app=test', got '%s'", selector)
	}
}

func TestMultiContainerCache_GetSet(t *testing.T) {
	cache := NewMultiContainerCache()

	// Test setting and getting
	cache.Set("pod1", true)

	result, exists := cache.Get("pod1")
	if !exists {
		t.Error("Expected to find cached value")
	}
	if !result {
		t.Error("Expected true, got false")
	}

	// Test getting non-existent
	_, exists = cache.Get("nonexistent")
	if exists {
		t.Error("Expected not to find cached value")
	}
}

func TestMultiContainerCache_Clear(t *testing.T) {
	cache := NewMultiContainerCache()

	cache.Set("pod1", true)
	cache.Set("pod2", false)

	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", cache.Size())
	}
}

func TestMultiContainerCache_ConcurrentAccess(t *testing.T) {
	cache := NewMultiContainerCache()
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			cache.Set("pod", n%2 == 0)
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Get("pod")
			cache.Size()
		}()
	}

	wg.Wait()

	// Verify cache is still functional
	_, exists := cache.Get("pod")
	if !exists {
		t.Error("Expected to find cached value after concurrent access")
	}
}
