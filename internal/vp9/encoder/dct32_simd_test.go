//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"math"
	"math/rand"
	"slices"
	"testing"
)

func TestForwardDCT32NativeParity(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("native SIMD disabled")
	}
	rng := rand.New(rand.NewSource(3216))
	for trial := range 1424 {
		stride := []int{32, 33, 47, 71}[trial%4]
		align := trial % 8
		backing := make([]int16, align+31*stride+32)
		in := backing[align:]
		for y := range 32 {
			for x := range 32 {
				v := rng.Intn(511) - 255
				switch trial {
				case 0:
					v = 0
				case 1:
					v = 255
				case 2:
					v = -255
				case 3:
					v = 255
					if (x+y)%2 == 0 {
						v = -255
					}
				case 4:
					v = 255
					if x%2 == 0 {
						v = -255
					}
				case 5:
					v = 255
					if y%2 == 0 {
						v = -255
					}
				case 6:
					v = 0
					if x == 31 && y == 31 {
						v = 255
					}
				case 7:
					v = 0
					if x == 0 && y == 0 {
						v = -255
					}
				}
				if trial >= 400 {
					k, l := (trial-400)/32, (trial-400)%32
					v = 255
					if math.Cos(math.Pi*float64((2*x+1)*k)/64)*math.Cos(math.Pi*float64((2*y+1)*l)/64) < 0 {
						v = -255
					}
				}
				in[y*stride+x] = int16(v)
			}
		}
		saved := slices.Clone(in)
		for _, rd := range []bool{false, true} {
			var want [1024]int16
			if rd {
				forwardDCT32x32RDScalar(in, stride, want[:])
			} else {
				forwardDCT32x32Scalar(in, stride, want[:])
			}
			var buf [1026]int16
			buf[0] = 12345
			buf[1025] = -12345
			if !forwardDCT32Go(in, stride, buf[1:1025], rd) {
				t.Fatalf("native path not selected trial%d rd%v", trial, rd)
			}
			if !slices.Equal(buf[1:1025], want[:]) {
				for i, v := range want {
					if buf[i+1] != v {
						t.Fatalf("trial%d rd%v coeff%d got%d want%d", trial, rd, i, buf[i+1], v)
					}
				}
			}
			if buf[0] != 12345 || buf[1025] != -12345 || !slices.Equal(in, saved) {
				t.Fatal("outside output or input modified")
			}
			alias := slices.Clone(in)
			if !forwardDCT32Go(alias, stride, alias[:1024], rd) || !slices.Equal(alias[:1024], want[:]) {
				t.Fatalf("in-place trial%d rd%v", trial, rd)
			}
		}
	}
}

func TestForwardDCT32NativeFallback(t *testing.T) {
	for _, v := range []int16{-32768, -256, 256, 32767} {
		for _, pos := range []int{0, 1023} {
			in := make([]int16, 1024)
			in[pos] = v
			for _, rd := range []bool{false, true} {
				out := slices.Repeat([]int16{12345}, 1024)
				if forwardDCT32Go(in, 32, out, rd) {
					t.Fatal("out-of-range input accepted")
				}
				for _, c := range out {
					if c != 12345 {
						t.Fatal("fallback modified output")
					}
				}
				var want [1024]int16
				if rd {
					forwardDCT32x32RDScalar(in, 32, want[:])
					ForwardDCT32x32RDInto(in, 32, out)
				} else {
					forwardDCT32x32Scalar(in, 32, want[:])
					ForwardDCT32x32Into(in, 32, out)
				}
				if !slices.Equal(out, want[:]) {
					t.Fatal("full-range public fallback mismatch")
				}
			}
		}
	}
}
