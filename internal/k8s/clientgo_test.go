package k8s

import (
	"context"
	"testing"
	"time"
)

// TestClientGoClient_Integration tests ClientGoClient against a real cluster
// Run with: go test -v ./internal/k8s -short=false
func TestClientGoClient_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create client using default kubeconfig context
	client, err := NewClientGoClient("")
	if err != nil {
		t.Fatalf("Failed to create ClientGoClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test namespace (assuming "default" exists)
	testNamespace := "default"

	t.Run("ListDeployments", func(t *testing.T) {
		deployments, err := client.ListDeployments(ctx, testNamespace)
		if err != nil {
			t.Errorf("ListDeployments failed: %v", err)
			return
		}
		t.Logf("Found %d deployments in namespace %s", len(deployments), testNamespace)
	})

	t.Run("ListPods", func(t *testing.T) {
		// List all pods (no selector)
		data, err := client.ListPods(ctx, testNamespace, "")
		if err != nil {
			t.Errorf("ListPods failed: %v", err)
			return
		}
		if len(data) == 0 {
			t.Log("No pods found in namespace")
		} else {
			t.Logf("Retrieved pod list data: %d bytes", len(data))
		}
	})

	t.Run("GetEvents", func(t *testing.T) {
		data, err := client.GetEvents(ctx, testNamespace)
		if err != nil {
			t.Errorf("GetEvents failed: %v", err)
			return
		}
		if len(data) == 0 {
			t.Log("No events found in namespace")
		} else {
			t.Logf("Retrieved events data: %d bytes", len(data))
		}
	})
}

// TestClientGoClient_ContextHandling tests context switching
func TestClientGoClient_ContextHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test with empty context (should use default)
	t.Run("DefaultContext", func(t *testing.T) {
		client, err := NewClientGoClient("")
		if err != nil {
			t.Errorf("Failed to create client with default context: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Simple operation to verify connection
		_, err = client.ListDeployments(ctx, "default")
		if err != nil {
			t.Logf("ListDeployments with default context: %v", err)
		} else {
			t.Log("Successfully connected with default context")
		}
	})
}

// TestClientGoClient_ErrorHandling tests error scenarios
func TestClientGoClient_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		t.Fatalf("Failed to create ClientGoClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("NonexistentNamespace", func(t *testing.T) {
		// This should not error - it will just return empty list
		deployments, err := client.ListDeployments(ctx, "nonexistent-namespace-12345")
		if err != nil {
			t.Logf("ListDeployments on nonexistent namespace returned error: %v", err)
		} else {
			t.Logf("ListDeployments on nonexistent namespace returned %d deployments", len(deployments))
		}
	})

	t.Run("NonexistentDeployment", func(t *testing.T) {
		_, err := client.GetDeployment(ctx, "default", "nonexistent-deployment-12345")
		if err == nil {
			t.Error("Expected error for nonexistent deployment, got nil")
		} else {
			t.Logf("Correctly received error for nonexistent deployment: %v", err)
		}
	})

	t.Run("ContextTimeout", func(t *testing.T) {
		// Create context with very short timeout
		shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(10 * time.Millisecond) // Ensure context expires

		_, err := client.ListDeployments(shortCtx, "default")
		if err == nil {
			t.Error("Expected timeout error, got nil")
		} else {
			t.Logf("Correctly received timeout error: %v", err)
		}
	})
}

// TestClientGoClient_OperationTypes tests different operation types
func TestClientGoClient_OperationTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		t.Fatalf("Failed to create ClientGoClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testNamespace := "default"

	t.Run("DeploymentOperations", func(t *testing.T) {
		deployments, err := client.ListDeployments(ctx, testNamespace)
		if err != nil {
			t.Errorf("ListDeployments failed: %v", err)
			return
		}

		if len(deployments) > 0 {
			// Test GetDeployment on first deployment
			t.Logf("Testing GetDeployment with: %s", deployments[0])
			data, err := client.GetDeployment(ctx, testNamespace, deployments[0])
			if err != nil {
				t.Errorf("GetDeployment failed: %v", err)
			} else {
				t.Logf("Successfully retrieved deployment data: %d bytes", len(data))
			}
		} else {
			t.Log("No deployments to test GetDeployment")
		}
	})

	t.Run("ResourceOperations", func(t *testing.T) {
		// Note: These may fail if resources don't exist, which is expected
		_, err := client.GetSecret(ctx, testNamespace, "default-token")
		if err != nil {
			t.Logf("GetSecret: %v (expected if secret doesn't exist)", err)
		}

		_, err = client.GetConfigMap(ctx, testNamespace, "kube-root-ca.crt")
		if err != nil {
			t.Logf("GetConfigMap: %v (expected if configmap doesn't exist)", err)
		}
	})
}
