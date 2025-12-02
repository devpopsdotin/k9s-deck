package k8s

import (
	"context"
	"os/exec"
	"time"
)

// Constants
const (
	CommandTimeout     = 2 * time.Second
	LongCommandTimeout = 5 * time.Second
)

// Client is the interface for Kubernetes operations
type Client interface {
	// Deployment operations
	GetDeployment(ctx context.Context, namespace, name string) ([]byte, error)
	ScaleDeployment(ctx context.Context, namespace, name string, replicas int) error
	RestartDeployment(ctx context.Context, namespace, name string) error
	ListDeployments(ctx context.Context, namespace string) ([]string, error)

	// Pod operations
	ListPods(ctx context.Context, namespace, selector string) ([]byte, error)
	GetPodLogs(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error)
	GetPodContainers(ctx context.Context, namespace, podName string) ([]string, error)

	// Helm operations
	GetHelmHistory(ctx context.Context, namespace, releaseName string) ([]byte, error)
	RollbackHelm(ctx context.Context, namespace, releaseName string, revision int) error

	// Resource operations (Secrets, ConfigMaps)
	GetSecret(ctx context.Context, namespace, name string) ([]byte, error)
	GetConfigMap(ctx context.Context, namespace, name string) ([]byte, error)
	GetResource(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error)

	// Event operations
	GetEvents(ctx context.Context, namespace string) ([]byte, error)
}

// KubectlClient implements Client using kubectl CLI
type KubectlClient struct {
	Context string // Kubernetes context
}

// NewKubectlClient creates a new kubectl-based client
func NewKubectlClient(context string) *KubectlClient {
	return &KubectlClient{
		Context: context,
	}
}

// runCmd executes a command with timeout
func (c *KubectlClient) runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// runCmdWithTimeout executes a command with a specific timeout
func (c *KubectlClient) runCmdWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.runCmd(ctx, name, args...)
}
