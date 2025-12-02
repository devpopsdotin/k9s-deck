package k8s

import (
	"context"
	"testing"
	"time"
)

// Benchmark kubectl CLI vs client-go performance
// Run with: go test -bench=. -benchmem ./internal/k8s

// BenchmarkKubectlClient_GetDeployment benchmarks kubectl CLI approach
func BenchmarkKubectlClient_GetDeployment(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client := NewKubectlClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.GetDeployment(ctx, "default", "nonexistent")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.GetDeployment(ctx, "default", "nonexistent")
	}
}

// BenchmarkClientGoClient_GetDeployment benchmarks client-go approach
func BenchmarkClientGoClient_GetDeployment(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.GetDeployment(ctx, "default", "nonexistent")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.GetDeployment(ctx, "default", "nonexistent")
	}
}

// BenchmarkKubectlClient_ListDeployments benchmarks kubectl CLI list
func BenchmarkKubectlClient_ListDeployments(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client := NewKubectlClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.ListDeployments(ctx, "default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.ListDeployments(ctx, "default")
	}
}

// BenchmarkClientGoClient_ListDeployments benchmarks client-go list
func BenchmarkClientGoClient_ListDeployments(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.ListDeployments(ctx, "default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.ListDeployments(ctx, "default")
	}
}

// BenchmarkKubectlClient_ListPods benchmarks kubectl CLI pod listing
func BenchmarkKubectlClient_ListPods(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client := NewKubectlClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.ListPods(ctx, "default", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.ListPods(ctx, "default", "")
	}
}

// BenchmarkClientGoClient_ListPods benchmarks client-go pod listing
func BenchmarkClientGoClient_ListPods(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.ListPods(ctx, "default", "")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.ListPods(ctx, "default", "")
	}
}

// BenchmarkKubectlClient_GetEvents benchmarks kubectl CLI event fetching
func BenchmarkKubectlClient_GetEvents(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client := NewKubectlClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.GetEvents(ctx, "default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.GetEvents(ctx, "default")
	}
}

// BenchmarkClientGoClient_GetEvents benchmarks client-go event fetching
func BenchmarkClientGoClient_GetEvents(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping benchmark in short mode")
	}

	client, err := NewClientGoClient("")
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warmup
	_, _ = client.GetEvents(ctx, "default")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = client.GetEvents(ctx, "default")
	}
}
