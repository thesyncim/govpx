//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"simd/archsimd"
	"testing"
)

func TestInternalEdgesPreserveSequentialInt16Results(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	rng := rand.New(rand.NewPCG(0x152684, 0x736209))
	for _, pitch := range []int{8, 9, 15, 16, 17, 32} {
		for _, vertical := range []bool{false, true} {
			if !vertical && pitch < 16 {
				continue
			}
			step, outer := pitch, 1
			if vertical {
				step, outer = 1, pitch
			}
			for _, align := range []int{0, 1, 7} {
				for trial := range 128 {
					original := make([]byte, align+15*pitch+16)
					for i := range original {
						if trial%2 == 0 {
							original[i] = 112 + byte(rng.UintN(17))
						} else {
							original[i] = byte(rng.Uint32())
						}
					}
					blimit := []int{0, 16, 64, 128, 254}[trial%5]
					limit := []int{0, 4, 32, 255}[(trial/5)%4]
					thresh := []int{0, 1, 8, 255}[(trial/20)%4]
					got, want := bytes.Clone(original), bytes.Clone(original)
					if !LoopFilterInternal16(got[align:], step, outer, blimit, limit, thresh) {
						t.Fatal("native internal dispatch unavailable")
					}
					// The independent int16 network executes each original edge in order.
					// Eight-position halves cannot affect the next half at these pitches.
					for edge := 1; edge <= 3; edge++ {
						for base := 0; base < 16; base += 8 {
							loopFilterNarrow8(want, align+4*edge*step+base*outer, step, outer, blimit, limit, thresh, 4)
						}
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("pitch%d vertical%v align%d trial%d blimit%d limit%d thresh%d", pitch, vertical, align, trial, blimit, limit, thresh)
					}
				}
			}
		}
	}
}

func TestInternalLoopFilterMinimalWindows(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for _, stride := range []int{8, 9, 15, 16, 17, 32} {
		for _, vertical := range []bool{false, true} {
			if stride < 16 && !vertical {
				continue
			}
			step, outer := stride, 1
			if vertical {
				step, outer = 1, stride
			}
			for align := range 8 {
				for trial := range 12 {
					original := make([]byte, align+15*stride+16)
					for i := range original {
						original[i] = byte(i*37 + trial*19)
						if trial%2 == 0 {
							original[i] = byte(112 + (i/stride+i%stride+trial)%17)
						}
					}
					for _, blimit := range []int{0, 64, 254, 255} {
						got, want := bytes.Clone(original), bytes.Clone(original)
						filtered := LoopFilterInternal16(got[align:], step, outer, blimit, 32, 0)
						if blimit == 255 {
							if filtered || !bytes.Equal(got, original) {
								t.Fatal("255 boundary must fall back without writes")
							}
							continue
						}
						if !filtered {
							t.Fatal("native dispatch not selected")
						}
						for edge := 1; edge <= 3; edge++ {
							LoopFilter(want[align:], 4*edge*step, step, outer, 16, blimit, 32, 0, 4)
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("stride%d vertical%v align%d trial%d blimit%d", stride, vertical, align, trial, blimit)
						}
					}
				}
			}
		}
	}
}

func TestInternalLoopFilterRejectInvalidSpans(t *testing.T) {
	p := bytes.Repeat([]byte{128}, 256)
	saved := bytes.Clone(p)
	maxInt := int(^uint(0) >> 1)
	for _, shape := range [][2]int{{maxInt, 1}, {1, maxInt}, {16, 1}, {1, 16}} {
		source := p
		if shape[0] == 16 || shape[1] == 16 {
			source = p[:255]
		}
		if LoopFilterInternal16(source, shape[0], shape[1], 64, 32, 0) || !bytes.Equal(p, saved) {
			t.Fatalf("invalid span accepted or changed: %v", shape)
		}
	}
	if LoopFilterInternal16(nil, 16, 1, 64, 32, 0) {
		t.Fatal("nil tile accepted")
	}
}

func TestLoopFilterNonFlatPacketParity(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	rng := rand.New(rand.NewPCG(9173, 2214))
	for _, kind := range []int{8, 16} {
		for _, n := range []int{8, 16} {
			for _, shape := range [][2]int{{n, 1}, {n + 3, 1}, {1, 16}, {1, 19}, {4, 1}, {1, 4}} {
				step, outer := shape[0], shape[1]
				for align := range 4 {
					off := align + 8*step
					for trial := range 32 {
						original := make([]byte, off+7*step+(n-1)*outer+1)
						for i := range original {
							original[i] = byte(rng.Uint32())
						}
						if trial%4 == 0 {
							for i := range original {
								original[i] = 128
							}
						}
						if trial%4 == 1 {
							for i := range original {
								original[i] = byte(120 + rng.IntN(17))
							}
						}
						if trial%4 == 2 {
							// Exactly one flat position must reject the whole fast packet.
							for i := -8; i <= 7; i++ {
								original[off+i*step] = 128
							}
						}
						for _, blimit := range []int{0, 64, 128, 254, 255} {
							got, want := bytes.Clone(original), bytes.Clone(original)
							LoopFilter(got, off, step, outer, n, blimit, 32, 8, kind)
							for base := 0; base < n; base += 8 {
								loopFilter8(want, off+base*outer, step, outer, blimit, 32, 8, kind)
							}
							if !bytes.Equal(got, want) {
								t.Fatalf("kind=%d n=%d shape=%v align=%d trial=%d blimit=%d", kind, n, shape, align, trial, blimit)
							}
						}
					}
				}
			}
		}
	}
}

func TestLoopFilterNonFlatRejectDoesNotWrite(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	original := bytes.Repeat([]byte{128}, 128)
	for _, limit := range []int{0, 32, 255} {
		got := bytes.Clone(original)
		if loopFilterNonFlat(got, 64, 16, 1, 64, limit, 0) || !bytes.Equal(got, original) {
			t.Fatal("flat packet changed before fallback")
		}
	}
}

func TestLoopFilterByteArithmetic(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for base := -128; base < 128; base += 16 {
		var values [16]int8
		for i := range values {
			values[i] = int8(base + i)
		}
		v := archsimd.LoadInt8x16Array(&values)
		var one, three [16]int8
		lfByteShift1(v).StoreArray(&one)
		lfByteShift3(v).StoreArray(&three)
		for i, x := range values {
			if one[i] != x>>1 || three[i] != x>>3 {
				t.Fatalf("byte shift %d: %d %d", x, one[i], three[i])
			}
		}
		for delta := -255; delta <= 255; delta++ {
			p, q := -128, -128+delta
			if delta < 0 {
				p, q = 127, 127+delta
			}
			var got [16]int8
			lfFilterDelta(v, archsimd.BroadcastInt8x16(int8(p)), archsimd.BroadcastInt8x16(int8(q))).StoreArray(&got)
			for i, outer := range values {
				want := int8(max(-128, min(127, int(outer)+3*delta)))
				if got[i] != want {
					t.Fatalf("outer=%d delta=%d: %d, want %d", outer, delta, got[i], want)
				}
			}
		}
	}
}

func TestLoopFilterEdgeTileBoundsAndWrites(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	for _, width := range []int{4, 8} {
		for _, pitch := range []int{width, width + 3, 31} {
			for align := range 4 {
				t.Run(fmt.Sprintf("width%d/pitch%d/align%d", width, pitch, align), func(t *testing.T) {
					original := make([]byte, align+7*pitch+width)
					for i := range original {
						original[i] = byte(i*37 + 11)
					}
					var a [16]archsimd.Int16x8
					const first = 4
					loadEdgeTile(original, align, pitch, width, &a, first)
					for x := range width {
						var lanes [8]int16
						a[first+x].Store(lanes[:])
						for y, v := range lanes {
							if want := original[align+y*pitch+x]; v != int16(want) {
								t.Fatalf("column %d row %d: %d != %d", x, y, v, want)
							}
						}
						// The expected rows differ in every column, so swapped transpose
						// lanes and writes outside the filter window cannot pass silently.
						a[first+x] = a[first+x].Add(archsimd.BroadcastInt16x8(13)).Min(archsimd.BroadcastInt16x8(255))
					}
					for _, count := range []int{2, 4, 6} {
						if count > width {
							continue
						}
						offset := (width - count) / 2
						got, want := bytes.Clone(original), bytes.Clone(original)
						for y := range 8 {
							for x := offset; x < offset+count; x++ {
								i := align + y*pitch + x
								want[i] = byte(min(int(want[i])+13, 255))
							}
						}
						storeEdgeTile(got, align, pitch, first, width, first+offset, first+offset+count, &a)
						if !bytes.Equal(got, want) {
							t.Fatalf("store count %d changed wrong bytes", count)
						}
					}
				})
			}
		}
	}
}

// Compare the wide byte/AVX2 dispatch against the int16 kernel with exact
// final-row allocations, including the 255 block-limit fallback boundary.
func TestLoopFilterMinimalWindows(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	seed := uint32(0x531728)
	next := func() byte { seed = seed*1664525 + 1013904223; return byte(seed >> 24) }
	for _, kind := range []int{2, 4, 6} {
		left, right := 4, 3
		if kind == 2 {
			left, right = 2, 1
		}
		for _, n := range []int{8, 16} {
			for _, layout := range [][2]int{{n + 3, 1}, {1, left + right + 4}, {1, 1}, {3, 1}, {1, 3}} {
				step, outer := layout[0], layout[1]
				for align := range 4 {
					off := align + left*step
					for trial := range 12 {
						original := make([]byte, off+right*step+(n-1)*outer+1)
						for i := range original {
							if trial%3 == 0 {
								original[i] = 120 + next()%9
							} else {
								original[i] = next()
							}
						}
						for _, blimit := range []int{0, 1, 64, 254, 255} {
							limit, thresh := 255, 8
							if trial%2 == 0 {
								limit, thresh = 32, 0
							}
							got, want := bytes.Clone(original), bytes.Clone(original)
							if !LoopFilter(got, off, step, outer, n, blimit, limit, thresh, kind) {
								t.Fatal("native dispatch unavailable")
							}
							for base := 0; base < n; base += 8 {
								loopFilterNarrow8(want, off+base*outer, step, outer, blimit, limit, thresh, kind)
							}
							if !bytes.Equal(got, want) {
								t.Fatalf("kind%d n%d step%d outer%d align%d trial%d blimit%d", kind, n, step, outer, align, trial, blimit)
							}
						}
					}
				}
			}
		}
	}
}

func TestLoopFilterUVNativeSelected(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD disabled")
	}
	for _, vertical := range []bool{false, true} {
		for _, stride := range []int{8, 9, 17} {
			step, outer := stride, 1
			if vertical {
				step, outer = 1, stride
			}
			for align := range 8 {
				for _, kind := range []int{4, 6} {
					u, v := make([]byte, align+7*stride+8), make([]byte, align+7*stride+8)
					for i := range u {
						u[i] = byte(112 + i%17)
						v[i] = byte(117 + i%13)
					}
					if !LoopFilterUV8(u[align:], v[align:], step, outer, 64, 32, 0, kind) {
						t.Fatal("native paired filter not selected")
					}
					saved := bytes.Clone(u)
					if LoopFilterUV8(u[align:], u[align:], step, outer, 64, 32, 0, kind) || !bytes.Equal(u, saved) {
						t.Fatal("overlap fallback modified plane")
					}
					savedV := bytes.Clone(v)
					if LoopFilterUV8(u[align:], v[align:], step, outer, 255, 32, 0, kind) || !bytes.Equal(u, saved) || !bytes.Equal(v, savedV) {
						t.Fatal("255 fallback modified planes")
					}
				}
			}
		}
	}
}

func TestLoopFilterUVRejectOversizedStrides(t *testing.T) {
	u, v := make([]byte, 64), make([]byte, 64)
	maxInt := int(^uint(0) >> 1)
	for _, stride := range []int{maxInt, maxInt/7 + 1} {
		if LoopFilterUV8(u, v, stride, 1, 64, 32, 0, 4) || LoopFilterUV8(u, v, 1, stride, 64, 32, 0, 6) {
			t.Fatal("oversized stride accepted")
		}
	}
}
