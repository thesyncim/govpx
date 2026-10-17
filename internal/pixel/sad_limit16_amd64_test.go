//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSADLimit16PointerMatchesRows(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, ss := range []int{0, 1, 15, 16, 19, 1312} {
		for mode := range 3 {
			rs := ss + 3
			a, b := make([]byte, 3+15*ss+16)[3:], make([]byte, 5+15*rs+16)[5:]
			for i := range a {
				if mode == 0 {
					a[i] = byte(i*37 + 11)
				} else if mode == 1 {
					a[i] = 255
				}
			}
			for i := range b {
				if mode == 0 {
					b[i] = byte(i*19 + 53)
				} else if mode == 2 {
					b[i] = 255
				}
			}
			ac, bc := bytes.Clone(a), bytes.Clone(b)
			limits := []int{-1, 0, 1024, 65279, 65280, 65281, maxInt}
			for _, h := range []int{1, 2, 4, 8, 15} {
				rowSum := SAD16LimitScalar(a, ss, b, rs, h, maxInt)
				limits = append(limits, rowSum-1, rowSum, rowSum+1)
			}
			for _, limit := range limits {
				want := SAD16LimitScalar(a, ss, b, rs, 16, limit)
				got := SAD16Limit16Ptr(&a[0], ss, &b[0], rs, limit)
				if got != want {
					t.Fatalf("stride%d mode%d limit%d got%d want%d", ss, mode, limit, got, want)
				}
			}
			if !bytes.Equal(a, ac) || !bytes.Equal(b, bc) {
				t.Fatal("input changed")
			}
		}
	}
	for _, stride := range []int{0, 1, 16, 19} {
		data := make([]byte, 5+15*stride+16)
		for i := range data {
			data[i] = byte(i*37 + 11)
		}
		a, b := data[3:], data[5:]
		for _, limit := range []int{-1, 1024, 65279, 65280, maxInt} {
			want := SAD16LimitScalar(a, stride, b, stride, 16, limit)
			if got := SAD16Limit16Ptr(&a[0], stride, &b[0], stride, limit); got != want {
				t.Fatalf("overlap stride%d got%d want%d", stride, got, want)
			}
		}
	}
}

func TestSADLimit16PointerDisabledBackwards(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	var a, b [256]byte
	for i := range a {
		a[i] = byte(i*37 + 11)
		b[i] = byte(i*19 + 53)
	}
	for _, limit := range []int{-1, 0, 1024, 65279, 65280, int(^uint(0) >> 1)} {
		want := 0
		for y := range 16 {
			for x := range 16 {
				d := int(a[240-y*16+x]) - int(b[240-y*16+x])
				if d < 0 {
					d = -d
				}
				want += d
			}
			if want > limit {
				break
			}
		}
		if got := SAD16Limit16Ptr(&a[240], -16, &b[240], -16, limit); got != want {
			t.Fatalf("limit%d got%d want%d", limit, got, want)
		}
	}
}

func TestSADLimit16PointerFallbackPanics(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD runtime disabled")
	}
	var block [16]byte
	maxInt := int(^uint(0) >> 1)
	// Every historical slice construction below panics before loading data.
	// These are invalid pointer windows, not probes into unallocated memory.
	cases := []struct {
		src *byte
		ss  int
		ref *byte
		rs  int
	}{
		{nil, 0, &block[0], 0}, {&block[0], 0, nil, 0},
		{&block[0], maxInt, &block[0], 0}, {&block[0], 0, &block[0], maxInt},
		{&block[0], (maxInt-16)/15 + 1, &block[0], 0},
		{&block[0], 0, &block[0], (maxInt-16)/15 + 1},
	}
	const probeKey = "GOVPX_SAD_LIMIT_PANIC_PROBE"
	if probe := os.Getenv(probeKey); probe != "" {
		for i, tc := range cases {
			if probe == fmt.Sprint(i) {
				_ = SAD16Limit16Ptr(tc.src, tc.ss, tc.ref, tc.rs, 65280)
				t.Fatal("invalid window unexpectedly returned")
				return
			}
		}
		t.Fatal("unknown panic probe")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// checkptr diagnoses invalid unsafe.Slice windows with an unrecoverable
	// runtime throw. A child verifies both that diagnostic and ordinary panic.
	for i := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			cmd := exec.Command(executable, "-test.run=^TestSADLimit16PointerFallbackPanics$")
			cmd.Env = append(os.Environ(), probeKey+"="+fmt.Sprint(i))
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "unsafe.Slice") {
				t.Fatalf("expected original slice-window failure: %v\n%s", err, output)
			}
		})
	}
}
