package uploadstatus

import "testing"

func TestStatusFromCounts(t *testing.T) {
	tests := []struct {
		name   string
		counts Counts
		want   string
	}{
		{
			name:   "no assets",
			counts: Counts{},
			want:   "pending",
		},
		{
			name:   "pending asset",
			counts: Counts{Total: 2, Pending: 1, Processed: 1},
			want:   "processing",
		},
		{
			name:   "uploaded asset",
			counts: Counts{Total: 1, Uploaded: 1},
			want:   "processing",
		},
		{
			name:   "processing asset",
			counts: Counts{Total: 1, Processing: 1},
			want:   "processing",
		},
		{
			name:   "all processed",
			counts: Counts{Total: 2, Processed: 2},
			want:   "processed",
		},
		{
			name:   "all failed",
			counts: Counts{Total: 2, Failed: 2},
			want:   "failed",
		},
		{
			name:   "processed and failed",
			counts: Counts{Total: 2, Processed: 1, Failed: 1},
			want:   "completed_with_errors",
		},
		{
			name:   "unexpected status",
			counts: Counts{Total: 1, Unexpected: 1},
			want:   "processing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusFromCounts(tt.counts); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
