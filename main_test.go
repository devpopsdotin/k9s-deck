package main

import "testing"

func TestIsDeploymentEvent(t *testing.T) {
	tests := []struct {
		kind, obj string
		want      bool
	}{
		{"Deployment", "api", true},
		{"ReplicaSet", "api-5c7588df9", true},
		{"Pod", "api-5c7588df9-zn5fd", true},
		// Other deployments sharing the "api" prefix
		{"Deployment", "api-gateway", false},
		{"ReplicaSet", "api-gateway-5c7588df9", false},
		{"Pod", "api-gateway-5c7588df9-zn5fd", false},
		// Unrelated kinds
		{"Service", "api", false},
	}
	for _, tt := range tests {
		if got := isDeploymentEvent(tt.kind, tt.obj, "api"); got != tt.want {
			t.Errorf("isDeploymentEvent(%q, %q, \"api\") = %v, want %v", tt.kind, tt.obj, got, tt.want)
		}
	}
}
