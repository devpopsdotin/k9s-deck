package k8s

import (
	"context"
	"fmt"
)

// MockClient is a mock implementation of the Client interface for testing
type MockClient struct {
	// Deployment operations
	GetDeploymentFunc    func(ctx context.Context, namespace, name string) ([]byte, error)
	ScaleDeploymentFunc  func(ctx context.Context, namespace, name string, replicas int) error
	RestartDeploymentFunc func(ctx context.Context, namespace, name string) error
	ListDeploymentsFunc  func(ctx context.Context, namespace string) ([]string, error)

	// Pod operations
	ListPodsFunc         func(ctx context.Context, namespace, selector string) ([]byte, error)
	GetPodLogsFunc       func(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error)
	GetPodContainersFunc func(ctx context.Context, namespace, podName string) ([]string, error)

	// Helm operations
	GetHelmHistoryFunc func(ctx context.Context, namespace, releaseName string) ([]byte, error)
	RollbackHelmFunc   func(ctx context.Context, namespace, releaseName string, revision int) error

	// Resource operations
	GetSecretFunc    func(ctx context.Context, namespace, name string) ([]byte, error)
	GetConfigMapFunc func(ctx context.Context, namespace, name string) ([]byte, error)
	GetResourceFunc  func(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error)

	// Event operations
	GetEventsFunc func(ctx context.Context, namespace string) ([]byte, error)
}

// NewMockClient creates a new mock client
func NewMockClient() *MockClient {
	return &MockClient{}
}

// Deployment operations

func (m *MockClient) GetDeployment(ctx context.Context, namespace, name string) ([]byte, error) {
	if m.GetDeploymentFunc != nil {
		return m.GetDeploymentFunc(ctx, namespace, name)
	}
	return nil, fmt.Errorf("GetDeploymentFunc not implemented")
}

func (m *MockClient) ScaleDeployment(ctx context.Context, namespace, name string, replicas int) error {
	if m.ScaleDeploymentFunc != nil {
		return m.ScaleDeploymentFunc(ctx, namespace, name, replicas)
	}
	return fmt.Errorf("ScaleDeploymentFunc not implemented")
}

func (m *MockClient) RestartDeployment(ctx context.Context, namespace, name string) error {
	if m.RestartDeploymentFunc != nil {
		return m.RestartDeploymentFunc(ctx, namespace, name)
	}
	return fmt.Errorf("RestartDeploymentFunc not implemented")
}

func (m *MockClient) ListDeployments(ctx context.Context, namespace string) ([]string, error) {
	if m.ListDeploymentsFunc != nil {
		return m.ListDeploymentsFunc(ctx, namespace)
	}
	return nil, fmt.Errorf("ListDeploymentsFunc not implemented")
}

// Pod operations

func (m *MockClient) ListPods(ctx context.Context, namespace, selector string) ([]byte, error) {
	if m.ListPodsFunc != nil {
		return m.ListPodsFunc(ctx, namespace, selector)
	}
	return nil, fmt.Errorf("ListPodsFunc not implemented")
}

func (m *MockClient) GetPodLogs(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error) {
	if m.GetPodLogsFunc != nil {
		return m.GetPodLogsFunc(ctx, namespace, podName, tailLines, allContainers, prefix)
	}
	return nil, fmt.Errorf("GetPodLogsFunc not implemented")
}

func (m *MockClient) GetPodContainers(ctx context.Context, namespace, podName string) ([]string, error) {
	if m.GetPodContainersFunc != nil {
		return m.GetPodContainersFunc(ctx, namespace, podName)
	}
	return nil, fmt.Errorf("GetPodContainersFunc not implemented")
}

// Helm operations

func (m *MockClient) GetHelmHistory(ctx context.Context, namespace, releaseName string) ([]byte, error) {
	if m.GetHelmHistoryFunc != nil {
		return m.GetHelmHistoryFunc(ctx, namespace, releaseName)
	}
	return nil, fmt.Errorf("GetHelmHistoryFunc not implemented")
}

func (m *MockClient) RollbackHelm(ctx context.Context, namespace, releaseName string, revision int) error {
	if m.RollbackHelmFunc != nil {
		return m.RollbackHelmFunc(ctx, namespace, releaseName, revision)
	}
	return fmt.Errorf("RollbackHelmFunc not implemented")
}

// Resource operations

func (m *MockClient) GetSecret(ctx context.Context, namespace, name string) ([]byte, error) {
	if m.GetSecretFunc != nil {
		return m.GetSecretFunc(ctx, namespace, name)
	}
	return nil, fmt.Errorf("GetSecretFunc not implemented")
}

func (m *MockClient) GetConfigMap(ctx context.Context, namespace, name string) ([]byte, error) {
	if m.GetConfigMapFunc != nil {
		return m.GetConfigMapFunc(ctx, namespace, name)
	}
	return nil, fmt.Errorf("GetConfigMapFunc not implemented")
}

func (m *MockClient) GetResource(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error) {
	if m.GetResourceFunc != nil {
		return m.GetResourceFunc(ctx, namespace, kind, name, outputFormat)
	}
	return nil, fmt.Errorf("GetResourceFunc not implemented")
}

// Event operations

func (m *MockClient) GetEvents(ctx context.Context, namespace string) ([]byte, error) {
	if m.GetEventsFunc != nil {
		return m.GetEventsFunc(ctx, namespace)
	}
	return nil, fmt.Errorf("GetEventsFunc not implemented")
}
