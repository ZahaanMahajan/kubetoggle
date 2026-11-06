package safety

import "testing"

func TestIsEligible(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		labels    map[string]string
		want      bool
	}{
		{
			name:      "toggleable deployment",
			namespace: "portfolio",
			labels:    map[string]string{ToggleLabel: "true"},
			want:      true,
		},
		{
			name:      "missing toggle label",
			namespace: "portfolio",
			labels:    map[string]string{},
			want:      false,
		},
		{
			name:      "toggle label false",
			namespace: "portfolio",
			labels:    map[string]string{ToggleLabel: "false"},
			want:      false,
		},
		{
			name:      "protected namespace",
			namespace: "argocd",
			labels:    map[string]string{ToggleLabel: "true"},
			want:      false,
		},
		{
			name:      "kube system namespace",
			namespace: "kube-system",
			labels:    map[string]string{ToggleLabel: "true"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEligible(tt.namespace, tt.labels)
			if got != tt.want {
				t.Fatalf("IsEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}
