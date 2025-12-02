package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/yaml"
)

// ClientGoClient implements Client interface using client-go
type ClientGoClient struct {
	clientset *kubernetes.Clientset
	context   string // kubeconfig context name
}

// NewClientGoClient creates a new client-go based client
func NewClientGoClient(kubeContext string) (*ClientGoClient, error) {
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")

	// Load config with specific context
	configLoadingRules := &clientcmd.ClientConfigLoadingRules{
		ExplicitPath: kubeconfig,
	}
	configOverrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		configOverrides.CurrentContext = kubeContext
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		configLoadingRules,
		configOverrides,
	).ClientConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &ClientGoClient{
		clientset: clientset,
		context:   kubeContext,
	}, nil
}

// ============================================================================
// Deployment Operations
// ============================================================================

// GetDeployment fetches deployment information as JSON
func (c *ClientGoClient) GetDeployment(ctx context.Context, namespace, name string) ([]byte, error) {
	slog.Debug("fetching deployment", "deployment", name, "namespace", namespace, "context", c.context)

	deployment, err := c.clientset.AppsV1().Deployments(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		slog.Error("failed to fetch deployment", "deployment", name, "namespace", namespace, "error", err)
		return nil, HandleK8sError(err, "deployment", name)
	}

	// Marshal to JSON to match interface contract
	data, err := json.Marshal(deployment)
	if err != nil {
		slog.Error("failed to marshal deployment", "deployment", name, "error", err)
		return nil, err
	}

	slog.Debug("deployment fetched successfully", "deployment", name, "bytes", len(data))
	return data, nil
}

// ScaleDeployment scales a deployment to the specified number of replicas
func (c *ClientGoClient) ScaleDeployment(ctx context.Context, namespace, name string, replicas int) error {
	slog.Info("scaling deployment", "deployment", name, "namespace", namespace, "replicas", replicas)

	// Get current scale
	scale, err := c.clientset.AppsV1().Deployments(namespace).GetScale(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		slog.Error("failed to get scale", "deployment", name, "error", err)
		return HandleK8sError(err, "deployment", name)
	}

	// Update replicas
	scale.Spec.Replicas = int32(replicas)

	_, err = c.clientset.AppsV1().Deployments(namespace).UpdateScale(
		ctx,
		name,
		scale,
		metav1.UpdateOptions{},
	)
	if err != nil {
		slog.Error("failed to scale deployment", "deployment", name, "error", err)
		return err
	}

	slog.Info("deployment scaled successfully", "deployment", name, "replicas", replicas)
	return nil
}

// RestartDeployment restarts a deployment (rollout restart)
func (c *ClientGoClient) RestartDeployment(ctx context.Context, namespace, name string) error {
	slog.Info("restarting deployment", "deployment", name, "namespace", namespace)

	// Patch with restartedAt annotation (kubectl rollout restart equivalent)
	patchData := []byte(fmt.Sprintf(
		`{"spec": {"template": {"metadata": {"annotations": {"kubectl.kubernetes.io/restartedAt": "%s"}}}}}`,
		time.Now().Format(time.RFC3339),
	))

	_, err := c.clientset.AppsV1().Deployments(namespace).Patch(
		ctx,
		name,
		types.StrategicMergePatchType,
		patchData,
		metav1.PatchOptions{},
	)
	if err != nil {
		slog.Error("failed to restart deployment", "deployment", name, "error", err)
		return err
	}

	slog.Info("deployment restarted successfully", "deployment", name)
	return nil
}

// ListDeployments lists all deployments in a namespace
func (c *ClientGoClient) ListDeployments(ctx context.Context, namespace string) ([]string, error) {
	slog.Debug("listing deployments", "namespace", namespace)

	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		slog.Error("failed to list deployments", "namespace", namespace, "error", err)
		return nil, err
	}

	// Extract names (replaces jsonpath)
	names := make([]string, len(deployments.Items))
	for i, deploy := range deployments.Items {
		names[i] = deploy.Name
	}

	slog.Debug("deployments listed", "namespace", namespace, "count", len(names))
	return names, nil
}

// ============================================================================
// Pod Operations
// ============================================================================

// ListPods lists pods in a namespace with optional label selector
func (c *ClientGoClient) ListPods(ctx context.Context, namespace, selector string) ([]byte, error) {
	slog.Debug("listing pods", "namespace", namespace, "selector", selector)

	pods, err := c.clientset.CoreV1().Pods(namespace).List(
		ctx,
		metav1.ListOptions{
			LabelSelector: selector,
		},
	)
	if err != nil {
		slog.Error("failed to list pods", "namespace", namespace, "error", err)
		return nil, err
	}

	// Marshal to JSON
	data, err := json.Marshal(pods)
	if err != nil {
		return nil, err
	}

	slog.Debug("pods listed", "namespace", namespace, "count", len(pods.Items))
	return data, nil
}

// GetPodLogs retrieves logs from a pod
func (c *ClientGoClient) GetPodLogs(ctx context.Context, namespace, podName string, tailLines int, allContainers, prefix bool) ([]byte, error) {
	var logs []byte

	if allContainers {
		// Get pod to enumerate containers
		pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		// Fetch logs for each container
		for _, container := range pod.Spec.Containers {
			tailLinesPtr := int64(tailLines)
			podLogOpts := &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: &tailLinesPtr,
			}

			stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts).Stream(ctx)
			if err != nil {
				continue // Skip failed containers
			}

			// Read all logs from stream
			containerLogs, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				continue
			}

			// Add prefix if requested
			if prefix {
				lines := strings.Split(string(containerLogs), "\n")
				for _, line := range lines {
					if line != "" {
						prefixed := fmt.Sprintf("[pod/%s/%s] %s\n", podName, container.Name, line)
						logs = append(logs, []byte(prefixed)...)
					}
				}
			} else {
				logs = append(logs, containerLogs...)
			}
		}
	} else {
		// Single container (or default)
		tailLinesPtr := int64(tailLines)
		podLogOpts := &corev1.PodLogOptions{
			TailLines: &tailLinesPtr,
		}

		stream, err := c.clientset.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts).Stream(ctx)
		if err != nil {
			return nil, err
		}
		defer stream.Close()

		logs, err = io.ReadAll(stream)
		if err != nil {
			return nil, err
		}
	}

	return logs, nil
}

// GetPodContainers retrieves the list of container names in a pod
func (c *ClientGoClient) GetPodContainers(ctx context.Context, namespace, podName string) ([]string, error) {
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(
		ctx,
		podName,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}

	// Extract container names (replaces jsonpath)
	names := make([]string, len(pod.Spec.Containers))
	for i, container := range pod.Spec.Containers {
		names[i] = container.Name
	}

	return names, nil
}

// ============================================================================
// Resource Operations (Secrets, ConfigMaps)
// ============================================================================

// GetSecret retrieves a secret as JSON
func (c *ClientGoClient) GetSecret(ctx context.Context, namespace, name string) ([]byte, error) {
	secret, err := c.clientset.CoreV1().Secrets(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}

	// Marshal to JSON (matches kubectl get secret -o json)
	return json.Marshal(secret)
}

// GetConfigMap retrieves a configmap as YAML
func (c *ClientGoClient) GetConfigMap(ctx context.Context, namespace, name string) ([]byte, error) {
	configMap, err := c.clientset.CoreV1().ConfigMaps(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, err
	}

	// Marshal to YAML (matches kubectl get configmap -o yaml)
	return yaml.Marshal(configMap)
}

// GetResource retrieves a generic resource (stub for now)
func (c *ClientGoClient) GetResource(ctx context.Context, namespace, kind, name, outputFormat string) ([]byte, error) {
	// For v2.1.0, most resources use typed methods
	// Dynamic client implementation can be added in future if needed
	return nil, fmt.Errorf("GetResource not yet implemented in client-go, use typed methods")
}

// ============================================================================
// Event Operations
// ============================================================================

// GetEvents retrieves events sorted by timestamp
func (c *ClientGoClient) GetEvents(ctx context.Context, namespace string) ([]byte, error) {
	events, err := c.clientset.CoreV1().Events(namespace).List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		return nil, err
	}

	// Sort by lastTimestamp (kubectl --sort-by=.lastTimestamp equivalent)
	sort.Slice(events.Items, func(i, j int) bool {
		return events.Items[i].LastTimestamp.Before(&events.Items[j].LastTimestamp)
	})

	// Marshal to JSON
	return json.Marshal(events)
}

// ============================================================================
// Helm Operations (Delegated to CLI - Hybrid Approach)
// ============================================================================

// GetHelmHistory fetches helm release history (uses CLI)
func (c *ClientGoClient) GetHelmHistory(ctx context.Context, namespace, releaseName string) ([]byte, error) {
	// Helm operations stay as CLI for v2.1.0 (no good Go SDK)
	// Delegate to KubectlClient
	kubectlClient := &KubectlClient{Context: c.context}
	return kubectlClient.GetHelmHistory(ctx, namespace, releaseName)
}

// RollbackHelm rolls back a helm release (uses CLI)
func (c *ClientGoClient) RollbackHelm(ctx context.Context, namespace, releaseName string, revision int) error {
	// Helm operations stay as CLI for v2.1.0 (no good Go SDK)
	// Delegate to KubectlClient
	kubectlClient := &KubectlClient{Context: c.context}
	return kubectlClient.RollbackHelm(ctx, namespace, releaseName, revision)
}
