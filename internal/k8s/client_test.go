package k8s

import (
	"context"
	"errors"
	"testing"
)

func TestMockClient_GetDeployment(t *testing.T) {
	mock := NewMockClient()

	expectedData := []byte(`{"kind":"Deployment","metadata":{"name":"test"}}`)
	mock.GetDeploymentFunc = func(ctx context.Context, namespace, name string) ([]byte, error) {
		if namespace == "default" && name == "test-deployment" {
			return expectedData, nil
		}
		return nil, errors.New("not found")
	}

	// Test success case
	data, err := mock.GetDeployment(context.Background(), "default", "test-deployment")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(data) != string(expectedData) {
		t.Errorf("Expected %s, got %s", expectedData, data)
	}

	// Test error case
	_, err = mock.GetDeployment(context.Background(), "other", "other-deployment")
	if err == nil {
		t.Error("Expected error, got nil")
	}
}

func TestMockClient_ScaleDeployment(t *testing.T) {
	mock := NewMockClient()

	scaleCalled := false
	mock.ScaleDeploymentFunc = func(ctx context.Context, namespace, name string, replicas int) error {
		scaleCalled = true
		if namespace != "default" || name != "test" || replicas != 3 {
			return errors.New("invalid parameters")
		}
		return nil
	}

	err := mock.ScaleDeployment(context.Background(), "default", "test", 3)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !scaleCalled {
		t.Error("ScaleDeploymentFunc was not called")
	}
}

func TestMockClient_RestartDeployment(t *testing.T) {
	mock := NewMockClient()

	restartCalled := false
	mock.RestartDeploymentFunc = func(ctx context.Context, namespace, name string) error {
		restartCalled = true
		if namespace != "default" || name != "test" {
			return errors.New("invalid parameters")
		}
		return nil
	}

	err := mock.RestartDeployment(context.Background(), "default", "test")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !restartCalled {
		t.Error("RestartDeploymentFunc was not called")
	}
}

func TestMockClient_ListDeployments(t *testing.T) {
	mock := NewMockClient()

	expectedDeployments := []string{"deploy1", "deploy2", "deploy3"}
	mock.ListDeploymentsFunc = func(ctx context.Context, namespace string) ([]string, error) {
		if namespace == "default" {
			return expectedDeployments, nil
		}
		return nil, errors.New("namespace not found")
	}

	deployments, err := mock.ListDeployments(context.Background(), "default")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(deployments) != len(expectedDeployments) {
		t.Errorf("Expected %d deployments, got %d", len(expectedDeployments), len(deployments))
	}
	for i, d := range deployments {
		if d != expectedDeployments[i] {
			t.Errorf("Expected deployment %s at index %d, got %s", expectedDeployments[i], i, d)
		}
	}
}

func TestMockClient_ListPods(t *testing.T) {
	mock := NewMockClient()

	expectedPods := []byte(`{"items":[{"metadata":{"name":"pod1"}},{"metadata":{"name":"pod2"}}]}`)
	mock.ListPodsFunc = func(ctx context.Context, namespace, selector string) ([]byte, error) {
		if selector == "app=test" {
			return expectedPods, nil
		}
		return nil, errors.New("no pods found")
	}

	pods, err := mock.ListPods(context.Background(), "default", "app=test")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(pods) != string(expectedPods) {
		t.Errorf("Expected %s, got %s", expectedPods, pods)
	}
}

func TestMockClient_GetPodLogs(t *testing.T) {
	mock := NewMockClient()

	expectedLogs := []byte("log line 1\nlog line 2\n")
	mock.GetPodLogsFunc = func(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error) {
		if podName == "test-pod" && tailLines == 100 {
			return expectedLogs, nil
		}
		return nil, errors.New("pod not found")
	}

	logs, err := mock.GetPodLogs(context.Background(), "default", "test-pod", 100, true, false)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(logs) != string(expectedLogs) {
		t.Errorf("Expected %s, got %s", expectedLogs, logs)
	}
}

func TestMockClient_GetPodContainers(t *testing.T) {
	mock := NewMockClient()

	expectedContainers := []string{"nginx", "sidecar"}
	mock.GetPodContainersFunc = func(ctx context.Context, namespace, podName string) ([]string, error) {
		if podName == "test-pod" {
			return expectedContainers, nil
		}
		return nil, errors.New("pod not found")
	}

	containers, err := mock.GetPodContainers(context.Background(), "default", "test-pod")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(containers) != len(expectedContainers) {
		t.Errorf("Expected %d containers, got %d", len(expectedContainers), len(containers))
	}
}

func TestMockClient_GetHelmHistory(t *testing.T) {
	mock := NewMockClient()

	expectedHistory := []byte("REVISION\tSTATUS\n1\tdeployed\n2\tsuperseded\n")
	mock.GetHelmHistoryFunc = func(ctx context.Context, namespace, releaseName string) ([]byte, error) {
		if releaseName == "my-release" {
			return expectedHistory, nil
		}
		return nil, errors.New("release not found")
	}

	history, err := mock.GetHelmHistory(context.Background(), "default", "my-release")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(history) != string(expectedHistory) {
		t.Errorf("Expected %s, got %s", expectedHistory, history)
	}
}

func TestMockClient_RollbackHelm(t *testing.T) {
	mock := NewMockClient()

	rollbackCalled := false
	mock.RollbackHelmFunc = func(ctx context.Context, namespace, releaseName string, revision int) error {
		rollbackCalled = true
		if releaseName == "my-release" && revision == 1 {
			return nil
		}
		return errors.New("rollback failed")
	}

	err := mock.RollbackHelm(context.Background(), "default", "my-release", 1)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !rollbackCalled {
		t.Error("RollbackHelmFunc was not called")
	}
}

func TestMockClient_GetSecret(t *testing.T) {
	mock := NewMockClient()

	expectedSecret := []byte(`{"data":{"password":"cGFzc3dvcmQ="}}`)
	mock.GetSecretFunc = func(ctx context.Context, namespace, name string) ([]byte, error) {
		if name == "my-secret" {
			return expectedSecret, nil
		}
		return nil, errors.New("secret not found")
	}

	secret, err := mock.GetSecret(context.Background(), "default", "my-secret")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(secret) != string(expectedSecret) {
		t.Errorf("Expected %s, got %s", expectedSecret, secret)
	}
}

func TestMockClient_GetConfigMap(t *testing.T) {
	mock := NewMockClient()

	expectedCM := []byte("apiVersion: v1\nkind: ConfigMap\n")
	mock.GetConfigMapFunc = func(ctx context.Context, namespace, name string) ([]byte, error) {
		if name == "my-config" {
			return expectedCM, nil
		}
		return nil, errors.New("configmap not found")
	}

	cm, err := mock.GetConfigMap(context.Background(), "default", "my-config")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(cm) != string(expectedCM) {
		t.Errorf("Expected %s, got %s", expectedCM, cm)
	}
}

func TestMockClient_GetResource(t *testing.T) {
	mock := NewMockClient()

	expectedResource := []byte(`{"kind":"Pod"}`)
	mock.GetResourceFunc = func(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error) {
		if kind == "pod" && name == "my-pod" && outputFormat == "json" {
			return expectedResource, nil
		}
		return nil, errors.New("resource not found")
	}

	resource, err := mock.GetResource(context.Background(), "default", "pod", "my-pod", "json")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(resource) != string(expectedResource) {
		t.Errorf("Expected %s, got %s", expectedResource, resource)
	}
}

func TestMockClient_GetEvents(t *testing.T) {
	mock := NewMockClient()

	expectedEvents := []byte(`{"items":[{"type":"Normal","reason":"Started"}]}`)
	mock.GetEventsFunc = func(ctx context.Context, namespace string) ([]byte, error) {
		if namespace == "default" {
			return expectedEvents, nil
		}
		return nil, errors.New("namespace not found")
	}

	events, err := mock.GetEvents(context.Background(), "default")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if string(events) != string(expectedEvents) {
		t.Errorf("Expected %s, got %s", expectedEvents, events)
	}
}

func TestMockClient_NotImplemented(t *testing.T) {
	mock := NewMockClient()

	// Test that unimplemented functions return errors
	_, err := mock.GetDeployment(context.Background(), "default", "test")
	if err == nil {
		t.Error("Expected error for unimplemented GetDeployment, got nil")
	}

	_, err = mock.ListPods(context.Background(), "default", "app=test")
	if err == nil {
		t.Error("Expected error for unimplemented ListPods, got nil")
	}

	_, err = mock.GetEvents(context.Background(), "default")
	if err == nil {
		t.Error("Expected error for unimplemented GetEvents, got nil")
	}
}
