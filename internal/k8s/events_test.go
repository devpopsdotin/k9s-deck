package k8s

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventTimestamp(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	last := metav1.NewTime(base)
	micro := metav1.NewMicroTime(base.Add(time.Minute))
	first := metav1.NewTime(base.Add(2 * time.Minute))
	created := metav1.NewTime(base.Add(3 * time.Minute))

	tests := []struct {
		name  string
		event corev1.Event
		want  time.Time
	}{
		{"lastTimestamp wins", corev1.Event{LastTimestamp: last, EventTime: micro}, last.Time},
		{"eventTime only", corev1.Event{EventTime: micro, FirstTimestamp: first}, micro.Time},
		{"firstTimestamp fallback", corev1.Event{FirstTimestamp: first}, first.Time},
		{"creation fallback", corev1.Event{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: created}}, created.Time},
	}
	for _, tt := range tests {
		if got := eventTimestamp(&tt.event); !got.Equal(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
