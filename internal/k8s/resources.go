package k8s

import (
	"context"
)

// GetSecret fetches a secret as JSON
func (c *KubectlClient) GetSecret(ctx context.Context, namespace, name string) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "get", "secret", name,
		"-n", namespace,
		"--context", c.Context,
		"-o", "json")
}

// GetConfigMap fetches a configmap as YAML
func (c *KubectlClient) GetConfigMap(ctx context.Context, namespace, name string) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "get", "configmap", name,
		"-n", namespace,
		"--context", c.Context,
		"-o", "yaml")
}

// GetResource is a generic method to fetch any Kubernetes resource
// kind: "deployment", "pod", "configmap", etc.
// outputFormat: "yaml", "json", etc.
func (c *KubectlClient) GetResource(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error) {
	return c.runCmd(ctx, "kubectl", "get", kind, name,
		"-n", namespace,
		"--context", c.Context,
		"-o", outputFormat)
}
