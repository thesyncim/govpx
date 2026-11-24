package govpx

import (
	"runtime"
	"testing"
)

func TestVP9RealtimeCBRAutoThreadHintMatchesCore(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{name: "default_plain", width: 320, height: 180},
		{name: "svc_top", width: 640, height: 360},
		{name: "loaded_plain", width: 1280, height: 720},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := VP9RealtimeCBRAutoThreadHint(tc.width, tc.height)
			want := vp9RealtimeCBRAutoThreadHint(tc.width, tc.height,
				runtime.NumCPU())
			if got != want {
				t.Fatalf("VP9RealtimeCBRAutoThreadHint(%d, %d) = %d, want %d",
					tc.width, tc.height, got, want)
			}
		})
	}
}

func TestVP9RealtimeCBRAutoThreadHintForCPU(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		cpus          int
		want          int
	}{
		{name: "invalid_width", width: 0, height: 180, cpus: 8, want: 1},
		{name: "single_cpu", width: 1280, height: 720, cpus: 1, want: 1},
		{name: "default_plain_too_narrow", width: 320, height: 180, cpus: 8, want: 1},
		{name: "svc_top_two_tiles", width: 640, height: 360, cpus: 8, want: 2},
		{name: "loaded_plain_two_cpus", width: 1280, height: 720, cpus: 2, want: 2},
		{name: "loaded_plain_four_tiles", width: 1280, height: 720, cpus: 8, want: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vp9RealtimeCBRAutoThreadHint(tc.width, tc.height,
				tc.cpus); got != tc.want {
				t.Fatalf("vp9RealtimeCBRAutoThreadHint(%d, %d, %d) = %d, want %d",
					tc.width, tc.height, tc.cpus, got, tc.want)
			}
		})
	}
}
