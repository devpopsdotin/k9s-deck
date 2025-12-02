package k8s

import (
	"context"
	"fmt"
	"log/slog"
)

// GetHelmHistory fetches the history of a Helm release
func (c *KubectlClient) GetHelmHistory(ctx context.Context, namespace, releaseName string) ([]byte, error) {
	slog.Debug("fetching helm history", "release", releaseName, "namespace", namespace)
	data, err := c.runCmd(ctx, "helm", "history", releaseName,
		"-n", namespace,
		"--kube-context", c.Context)
	if err != nil {
		slog.Error("failed to fetch helm history", "release", releaseName, "error", err)
		return nil, err
	}
	slog.Debug("helm history fetched", "release", releaseName)
	return data, nil
}

// RollbackHelm rolls back a Helm release to a specific revision
func (c *KubectlClient) RollbackHelm(ctx context.Context, namespace, releaseName string, revision int) error {
	slog.Info("rolling back helm release", "release", releaseName, "revision", revision)
	_, err := c.runCmd(ctx, "helm", "rollback", releaseName, fmt.Sprintf("%d", revision),
		"-n", namespace,
		"--kube-context", c.Context)
	if err != nil {
		slog.Error("failed to rollback helm release", "release", releaseName, "error", err)
		return err
	}
	slog.Info("helm release rolled back successfully", "release", releaseName, "revision", revision)
	return nil
}
