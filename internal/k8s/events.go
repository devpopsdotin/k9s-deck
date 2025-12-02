package k8s

import (
	"context"
)

// GetEvents fetches Kubernetes events for a namespace, sorted by timestamp
func (c *KubectlClient) GetEvents(ctx context.Context, namespace string) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "get", "events",
		"-n", namespace,
		"--context", c.Context,
		"--sort-by=.lastTimestamp",
		"-o", "json")
}
