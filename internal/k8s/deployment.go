package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// GetDeployment fetches deployment information as JSON
func (c *KubectlClient) GetDeployment(ctx context.Context, namespace, name string) ([]byte, error) {
	slog.Debug("fetching deployment", "deployment", name, "namespace", namespace, "context", c.Context)
	data, err := c.runCmd(ctx, "kubectl", "get", "deployment", name,
		"-n", namespace,
		"--context", c.Context,
		"-o", "json")
	if err != nil {
		slog.Error("failed to fetch deployment", "deployment", name, "namespace", namespace, "error", err)
		return nil, err
	}
	slog.Debug("deployment fetched successfully", "deployment", name, "bytes", len(data))
	return data, nil
}

// ScaleDeployment scales a deployment to the specified number of replicas
func (c *KubectlClient) ScaleDeployment(ctx context.Context, namespace, name string, replicas int) error {
	slog.Info("scaling deployment", "deployment", name, "namespace", namespace, "replicas", replicas)
	_, err := c.runCmd(ctx, "kubectl", "scale", "deployment", name,
		"--replicas="+fmt.Sprintf("%d", replicas),
		"-n", namespace,
		"--context", c.Context)
	if err != nil {
		slog.Error("failed to scale deployment", "deployment", name, "error", err)
		return err
	}
	slog.Info("deployment scaled successfully", "deployment", name, "replicas", replicas)
	return nil
}

// RestartDeployment restarts a deployment
func (c *KubectlClient) RestartDeployment(ctx context.Context, namespace, name string) error {
	slog.Info("restarting deployment", "deployment", name, "namespace", namespace)
	_, err := c.runCmd(ctx, "kubectl", "rollout", "restart", "deployment", name,
		"-n", namespace,
		"--context", c.Context)
	if err != nil {
		slog.Error("failed to restart deployment", "deployment", name, "error", err)
		return err
	}
	slog.Info("deployment restarted successfully", "deployment", name)
	return nil
}

// ListDeployments lists all deployments in a namespace
func (c *KubectlClient) ListDeployments(ctx context.Context, namespace string) ([]string, error) {
	slog.Debug("listing deployments", "namespace", namespace)
	out, err := c.runCmd(ctx, "kubectl", "get", "deployments",
		"-n", namespace,
		"--context", c.Context,
		"-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		slog.Error("failed to list deployments", "namespace", namespace, "error", err)
		return nil, err
	}

	deployments := strings.Fields(strings.TrimSpace(string(out)))
	slog.Debug("deployments listed", "namespace", namespace, "count", len(deployments))
	return deployments, nil
}
