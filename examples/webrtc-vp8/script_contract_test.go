package main

import (
	"runtime"
	"testing"
)

func TestVP8WebRTCRenditionThreadProfile(t *testing.T) {
	cores := max(runtime.NumCPU(), 1)
	tests := []struct {
		name           string
		width          int
		height         int
		wantThreads    int
		wantTokenParts int
	}{
		{name: "low", width: 320, height: 180, wantThreads: 1, wantTokenParts: 0},
		{name: "mid", width: 640, height: 360, wantThreads: min(2, cores), wantTokenParts: pickTokenPartitions(min(2, cores))},
		{name: "high", width: 1280, height: 720, wantThreads: min(4, cores), wantTokenParts: pickTokenPartitions(min(4, cores))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			threads := pickThreads(tt.width, tt.height)
			if threads != tt.wantThreads {
				t.Fatalf("pickThreads(%dx%d) = %d, want %d", tt.width, tt.height, threads, tt.wantThreads)
			}
			if got := pickTokenPartitions(threads); got != tt.wantTokenParts {
				t.Fatalf("pickTokenPartitions(%d) = %d, want %d", threads, got, tt.wantTokenParts)
			}
		})
	}
}
