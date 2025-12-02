package k8s

import (
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// HandleK8sError provides user-friendly error messages for Kubernetes API errors
func HandleK8sError(err error, resource, name string) error {
	if err == nil {
		return nil
	}

	if k8serrors.IsNotFound(err) {
		return fmt.Errorf("%s '%s' not found", resource, name)
	}

	if k8serrors.IsForbidden(err) {
		return fmt.Errorf("permission denied accessing %s '%s'", resource, name)
	}

	if k8serrors.IsUnauthorized(err) {
		return fmt.Errorf("authentication failed")
	}

	if k8serrors.IsTimeout(err) || k8serrors.IsServerTimeout(err) {
		return fmt.Errorf("kubernetes API timeout")
	}

	if k8serrors.IsConflict(err) {
		return fmt.Errorf("%s '%s' was modified, please retry", resource, name)
	}

	if k8serrors.IsInvalid(err) {
		return fmt.Errorf("invalid %s specification: %v", resource, err)
	}

	// Return original error if no specific handling
	return err
}
