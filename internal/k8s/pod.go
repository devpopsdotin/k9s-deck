package k8s

import (
	"context"
	"fmt"
	"strings"
)

// ListPods fetches pods matching a label selector as JSON
func (c *KubectlClient) ListPods(ctx context.Context, namespace, selector string) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "get", "pods",
		"-n", namespace,
		"--context", c.Context,
		"-l", selector,
		"-o", "json")
}

// GetPodLogs fetches logs from a pod
func (c *KubectlClient) GetPodLogs(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error) {
	args := []string{"logs", podName,
		"-n", namespace,
		"--context", c.Context,
		fmt.Sprintf("--tail=%d", tailLines)}

	if allContainers {
		args = append(args, "--all-containers=true")
	}

	if prefix {
		args = append(args, "--prefix")
	}

	return c.runCmd(ctx, "kubectl", args...)
}

// GetPodContainers returns the list of container names in a pod
func (c *KubectlClient) GetPodContainers(ctx context.Context, namespace, podName string) ([]string, error) {
	out, err := c.runCmd(ctx, "kubectl", "get", "pod", podName,
		"-n", namespace,
		"--context", c.Context,
		"-o", "jsonpath={.spec.containers[*].name}")
	if err != nil {
		return nil, err
	}

	containerNames := strings.Fields(string(out))
	return containerNames, nil
}

// GetPodsBySelector fetches logs from all pods matching a selector
func (c *KubectlClient) GetPodsBySelector(ctx context.Context, namespace, selector string, tailLines int) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "logs",
		"-l", selector,
		"-n", namespace,
		"--context", c.Context,
		"--all-containers=true",
		"--prefix",
		fmt.Sprintf("--tail=%d", tailLines))
}
