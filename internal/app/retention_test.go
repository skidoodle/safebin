package app

import (
	"testing"
	"time"
)

func TestCalculateRetention(t *testing.T) {
	maxMB := int64(100)

	tests := []struct {
		name     string
		fileSize int64
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{
			name:     "Tiny file (Max retention)",
			fileSize: 1024,
			wantMin:  MaxRetention - time.Hour,
			wantMax:  MaxRetention,
		},
		{
			name:     "Max size file (Min retention)",
			fileSize: 100 * MegaByte,
			wantMin:  MinRetention,
			wantMax:  MinRetention + time.Minute,
		},
		{
			name:     "Half size file (Somewhere in between)",
			fileSize: 50 * MegaByte,
			wantMin: 24 * time.Hour,
			wantMax: MaxRetention,
		},
		{
			name:     "Oversized file (Min retention)",
			fileSize: 200 * MegaByte,
			wantMin:  MinRetention,
			wantMax:  MinRetention + time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateRetention(tc.fileSize, maxMB)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("Retention for size %d: got %v, want between %v and %v",
					tc.fileSize, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}
