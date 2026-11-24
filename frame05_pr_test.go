package govpx

import (
	"testing"
)

var frame05framePRSink int

func frame05frameRefdotArtifactCornerCheck(srcMB []byte, srcStride int, refMB []byte, refStride int, shift int) bool {
	corners := [4][4]int{
		{0, 0, 1, 1},
		{0, shift, 1, -1},
		{shift, 0, -1, 1},
		{shift, shift, -1, -1},
	}
	for _, c := range corners {
		gradLast := frame05frameRefmacroblockCornerGradient(refMB, refStride, c[0], c[1], c[2], c[3])
		gradSrc := frame05frameRefmacroblockCornerGradient(srcMB, srcStride, c[0], c[1], c[2], c[3])
		if gradLast >= dotArtifactCornerGradLast && gradSrc <= dotArtifactCornerGradSource {
			return true
		}
	}
	return false
}

func frame05frameRefmacroblockCornerGradient(plane []byte, stride int, offRow int, offCol int, sgnRow int, sgnCol int) int {
	y1 := int(plane[offRow*stride+offCol])
	y2 := int(plane[offRow*stride+offCol+sgnCol])
	y3 := int(plane[(offRow+sgnRow)*stride+offCol])
	y4 := int(plane[(offRow+sgnRow)*stride+offCol+sgnCol])
	// Branchless |delta| via sign-mask XOR plus max() to fold the
	// per-neighbor d{1,2,3} max ladder into builtin calls.
	d1 := y1 - y2
	m1 := d1 >> mvKernelSignShift
	d1 = (d1 ^ m1) - m1
	d2 := y1 - y3
	m2 := d2 >> mvKernelSignShift
	d2 = (d2 ^ m2) - m2
	d3 := y1 - y4
	m3 := d3 >> mvKernelSignShift
	d3 = (d3 ^ m3) - m3
	return max(max(d1, d2), d3)
}

func TestFramePR05(t *testing.T) {
	for seed := range 20 {
		a, z := make([]byte, 256), make([]byte, 256)
		for i := range a {
			a[i] = byte(i*seed + seed*7)
			z[i] = byte(i * (seed + 1))
		}
		for _, shift := range []int{7, 15} {
			if dotArtifactCornerCheck(a, 16, z, 16, shift) != frame05frameRefdotArtifactCornerCheck(a, 16, z, 16, shift) {
				t.Fatal("corner", seed, shift)
			}
		}
	}
}

func BenchmarkFramePR05(b *testing.B) {
	for _, kind := range []string{"flat-last", "gradient-last", "mixed"} {
		b.Run(kind, func(b *testing.B) {
			a, z := make([]byte, 256), make([]byte, 256)
			for i := range a {
				a[i] = 100
				z[i] = 100
				if kind == "gradient-last" {
					z[i] = byte(i * 7)
				}
				if kind == "mixed" {
					a[i] = byte(i * 29)
					z[i] = byte(i * 31)
				}
			}
			sum := 0
			b.ReportAllocs()
			for b.Loop() {
				if dotArtifactCornerCheck(a, 16, z, 16, 15) {
					sum++
				}
			}
			frame05framePRSink = sum
		})
	}
}
