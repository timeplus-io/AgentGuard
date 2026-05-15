package handlers

import "testing"

func TestShouldAutoReopenThreat(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		statusUpdatedAt string
		lastSeen        string
		want            bool
	}{
		{
			name:            "cleared threat reopens on later detection",
			status:          "cleared",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            true,
		},
		{
			name:            "acknowledged threat reopens on later detection",
			status:          "acknowledged",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            true,
		},
		{
			name:            "cleared threat stays cleared without later detection",
			status:          "cleared",
			statusUpdatedAt: "2026-05-13 09:49:37.679",
			lastSeen:        "2026-05-13 09:49:36.146",
			want:            false,
		},
		{
			name:            "acknowledged threat stays acknowledged without later detection",
			status:          "acknowledged",
			statusUpdatedAt: "2026-05-13 09:49:37.679",
			lastSeen:        "2026-05-13 09:49:36.146",
			want:            false,
		},
		{
			name:            "open threat is already open",
			status:          "open",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
		{
			name:            "brand-new threat is handled by empty-status path",
			status:          "",
			statusUpdatedAt: "1970-01-01 00:00:00.000",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
		{
			name:            "invalid timestamp does not reopen",
			status:          "cleared",
			statusUpdatedAt: "not-a-time",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoReopenThreat(tt.status, tt.statusUpdatedAt, tt.lastSeen)
			if got != tt.want {
				t.Fatalf("shouldAutoReopenThreat(%q, %q, %q) = %v, want %v",
					tt.status, tt.statusUpdatedAt, tt.lastSeen, got, tt.want)
			}
		})
	}
}
