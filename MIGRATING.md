# Migrating to v2.1.0

## For End Users

**No action required!** v2.1.0 is a drop-in replacement with identical behavior and UI. Simply download the new binary and replace your existing installation.

### What's New

- **5-10x Performance Improvement**: Operations that took 100-150ms now complete in 10-20ms
- **Lower Memory Usage**: ~30% reduction in memory consumption
- **Same User Experience**: All keyboard shortcuts, commands, and features work identically

### Installation

Follow the same installation steps as v2.0.0:

1. Download the v2.1.0 binary from [Releases](https://github.com/devpopsdotin/k9s-deck/releases)
2. Replace your existing `k9s-deck` binary
3. No configuration changes needed

---

## For Developers

If you're building from source or contributing to K9s Deck, here's what changed:

### Dependencies

New dependencies added in v2.1.0:

```bash
go get k8s.io/client-go@v0.34+
go get k8s.io/api@v0.34+
go get k8s.io/apimachinery@v0.34+
go get sigs.k8s.io/yaml@v1.4+
```

Run `go mod tidy` after pulling v2.1.0 to update dependencies automatically.

### Architecture Changes

**New Client Implementation**:

```go
// v2.0.0 - kubectl CLI wrapper
client := &k8s.KubectlClient{Context: context}

// v2.1.0 - Native client-go (recommended)
client, err := k8s.NewClientGoClient(context)
if err != nil {
    log.Fatal(err)
}

// v2.1.0 - Generic constructor (uses client-go by default)
client, err := k8s.NewClient(context)
```

**Interface Unchanged**: The `Client` interface remains the same (14 methods), ensuring backwards compatibility.

### New Files in v2.1.0

```
internal/k8s/
├── clientgo.go            # NEW: ClientGoClient implementation (~370 lines)
├── errors.go              # NEW: K8s error handling utilities
├── clientgo_test.go       # NEW: Integration tests
└── benchmark_test.go      # NEW: Performance benchmarks
```

### Running Tests

```bash
# Unit tests only (fast)
go test ./... -short

# Include integration tests (requires cluster access)
go test ./... -short=false

# Run benchmarks
go test -bench=. -benchmem ./internal/k8s

# Race detector
go test -race ./...
```

### Performance Expectations

| Operation | v2.0.0 (kubectl) | v2.1.0 (client-go) | Improvement |
|-----------|------------------|--------------------| ------------|
| GetDeployment | 100-150ms | 10-20ms | 5-10x |
| ListPods | 100-150ms | 10-20ms | 5-10x |
| GetPodLogs | 150-200ms | 20-30ms | 5-7x |
| Full Refresh (1 deploy) | 1.5-2s | 200-300ms | 5-7x |
| Memory (per refresh) | ~50MB | ~35MB | 30% reduction |

### Hybrid Architecture

v2.1.0 uses a hybrid approach:

- **client-go**: All kubectl operations (deployments, pods, secrets, configmaps, events)
- **CLI**: Helm operations (history, rollback)

**Reason**: No stable Helm Go SDK exists. Helm operations delegate to `KubectlClient` internally.

### Error Handling

New error mapping for better user feedback:

```go
import "github.com/devpopsdotin/k9s-deck/internal/k8s"

// Automatically maps Kubernetes errors to user-friendly messages
err := client.GetDeployment(ctx, "default", "nonexistent")
// Returns: "deployment 'nonexistent' not found"

// Instead of raw error:
// "deployments.apps \"nonexistent\" not found"
```

Error types handled:
- `IsNotFound` → "resource 'name' not found"
- `IsForbidden` → "permission denied accessing resource 'name'"
- `IsUnauthorized` → "authentication failed"
- `IsTimeout` → "kubernetes API timeout"
- `IsConflict` → "resource 'name' was modified, please retry"

### Backwards Compatibility

Both implementations coexist:

```go
// Legacy CLI wrapper (still available)
kubectlClient := k8s.NewKubectlClient(context)

// Native client-go (default in v2.1.0)
clientGoClient, _ := k8s.NewClientGoClient(context)

// Both implement the same Client interface
var client k8s.Client
client = kubectlClient  // ✅ Works
client = clientGoClient // ✅ Works
```

### Breaking Changes

**None.** v2.1.0 maintains 100% API compatibility with v2.0.0.

---

## Troubleshooting

### Build Errors

**Issue**: `no required module provides package k8s.io/client-go`

**Solution**: Run `go mod tidy` to download dependencies.

### Integration Test Failures

**Issue**: Integration tests fail with "Failed to create client"

**Solution**: Ensure valid kubeconfig exists at `~/.kube/config` and you have cluster access.

```bash
# Test cluster connection
kubectl cluster-info

# Run only unit tests (skip integration)
go test ./... -short
```

### Performance Not Improved

**Issue**: Performance similar to v2.0.0

**Possible Causes**:
1. Using `KubectlClient` instead of `ClientGoClient`
2. Network latency to API server (client-go can't optimize network)
3. API server performance issues

**Check**:
```bash
# Verify client-go is being used
go test -bench=BenchmarkClientGoClient ./internal/k8s -short=false
```

---

## Rollback to v2.0.0

If you encounter issues with v2.1.0:

### For End Users

1. Download v2.0.0 binary from [Releases](https://github.com/devpopsdotin/k9s-deck/releases/tag/v2.0.0)
2. Replace the v2.1.0 binary
3. Restart K9s

### For Developers

```bash
# Checkout v2.0.0 tag
git checkout v2.0.0

# Build
go build .
```

---

## Getting Help

- **Issues**: https://github.com/devpopsdotin/k9s-deck/issues
- **Discussions**: https://github.com/devpopsdotin/k9s-deck/discussions

When reporting issues, please include:
- K9s Deck version (`k9s-deck --version` if available)
- Kubernetes version (`kubectl version`)
- Operating system
- Error messages or logs (`/tmp/k9s-deck.log`)
