//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestDenoiseBytesScalar(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD unavailable")
	}
	seed := uint32(0x841651)
	for _, stride := range []int{16, 17, 23, 41} {
		for align := range 8 {
			for inc := range 2 {
				for _, level := range []int{3, 4, 5} {
					for trial := range 20 {
						size := 15*stride + 16
						mc, avg, sig := make([]byte, size+align)[align:], make([]byte, size+align)[align:], make([]byte, size+align)[align:]
						for i := range mc {
							seed = seed*1664525 + 1013904223
							mc[i] = byte(seed >> 24)
							seed = seed*1664525 + 1013904223
							sig[i] = byte(seed >> 24)
							avg[i] = 71
						}
						if trial < 4 {
							for y := range 16 {
								for x := range 16 {
									i := y*stride + x
									switch trial {
									case 0:
										mc[i], sig[i] = 255, 0
									case 1:
										mc[i], sig[i] = 0, 255
									case 2:
										mc[i], sig[i] = 255, 0
										if y == 15 {
											mc[i], sig[i] = 0, 255
										}
									case 3:
										mc[i] = sig[i]
									}
								}
							}
						}
						for _, alias := range []int{0, 1, 2} {
							m, s, a := bytes.Clone(mc), bytes.Clone(sig), bytes.Clone(avg)
							wm, ws, wa := bytes.Clone(mc), bytes.Clone(sig), bytes.Clone(avg)
							if alias == 1 {
								a = m
								wa = wm
							}
							if alias == 2 {
								a = s
								wa = ws
							}
							want := DenoiseFirstScalar(wm, stride, wa, stride, ws, stride, 16, inc, level)
							got, ok := denoiseFirst16Bytes(m, stride, a, stride, s, stride, inc, level)
							if !ok || got != want || !bytes.Equal(a, wa) || !bytes.Equal(m, wm) || !bytes.Equal(s, ws) {
								t.Fatalf("stride%d align%d inc%d level%d trial%d alias%d sum%d/%d selected%v", stride, align, inc, level, trial, alias, got, want, ok)
							}
						}
					}
				}
			}
		}
	}
}

func TestDenoiseBytesDeclinesAliasesAndShortRows(t *testing.T) {
	for _, stride := range []int{0, 1, 15, 16, 17, 23} {
		for _, off := range []int{0, 1, 7, 8, 17, 32} {
			backing := make([]byte, 1024)
			for i := range backing {
				backing[i] = byte(i*37 + 11)
			}
			original := bytes.Clone(backing)
			_, ok := denoiseFirst16Bytes(backing, stride, backing[off:], stride, backing[400:], stride, 1, 5)
			if stride < 16 || off != 0 {
				if ok || !bytes.Equal(backing, original) {
					t.Fatalf("stride%d off%d decline wrote or accepted", stride, off)
				}
			}
		}
	}
	const maxInt = int(^uint(0) >> 1)
	for _, shape := range [][3]int{{16, 17, 16}, {17, 16, 16}, {16, 16, 17}, {maxInt, 16, 16}, {16, maxInt, 16}, {16, 16, maxInt}, {-1, 16, 16}, {16, -1, 16}, {16, 16, -1}} {
		backing := bytes.Repeat([]byte{71}, 512)
		before := bytes.Clone(backing)
		if _, ok := denoiseFirst16Bytes(backing, shape[0], backing, shape[1], backing, shape[2], 1, 5); ok || !bytes.Equal(backing, before) {
			t.Fatalf("shape%v accepted/wrote", shape)
		}
	}

	for _, size := range []int{0, 1, 15, 16, 254, 255} {
		mc, avg, sig := make([]byte, size), bytes.Repeat([]byte{71}, 256), make([]byte, 256)
		before := bytes.Clone(avg)
		if _, ok := denoiseFirst16Bytes(mc, 16, avg, 16, sig, 16, 1, 5); ok || !bytes.Equal(avg, before) {
			t.Fatalf("size%d accepted/wrote", size)
		}
	}
}

// Partial aliases retain the preexisting eight-pixel load-before-store order.
func TestDenoiseBytesPacketAliasOrder(t *testing.T) {
	for _, stride := range []int{15, 16, 17, 23} {
		for _, offsets := range [][3]int{{0, 1, 400}, {0, 7, 400}, {0, 8, 400}, {0, 17, 400}, {0, 400, 401}, {0, 400, 407}, {0, 400, 408}, {0, 400, 417}, {0, 0, 1}} {
			for inc := range 2 {
				for _, level := range []int{3, 4, 5} {
					got := make([]byte, 2048)
					for i := range got {
						got[i] = byte(i*37 + 11)
					}
					want := bytes.Clone(got)
					m, a, s := offsets[0], offsets[1], offsets[2]
					var cols [16]int
					for y := range 16 {
						for x := 0; x < 16; x += 8 {
							var mp, sp [8]byte
							copy(mp[:], want[m+y*stride+x:])
							copy(sp[:], want[s+y*stride+x:])
							for i := range 8 {
								d := int(mp[i]) - int(sp[i])
								abs := max(d, -d)
								adj := level + 3
								if abs <= 15 {
									adj = level + 1
								}
								if abs <= 7 {
									adj = level
								}
								if abs <= 3+inc {
									adj = abs
								}
								if d < 0 {
									adj = -adj
								}
								cols[x+i] += adj
								want[a+y*stride+x+i] = byte(min(255, max(0, int(sp[i])+adj)))
							}
						}
					}
					sum := 0
					for _, v := range cols {
						sum += min(v, 127)
					}
					result := DenoiseFirst(got[m:], stride, got[a:], stride, got[s:], stride, 16, inc, level)
					if result != sum || !bytes.Equal(got, want) {
						t.Fatalf("stride%d offsets%v inc%d level%d sum%d/%d", stride, offsets, inc, level, result, sum)
					}
				}
			}
		}
	}
}

func TestDenoiseBytesDistinctStridesAndPublicDispatch(t *testing.T) {
	if !Enabled || !byteDenoise16 {
		t.Fatal("required ARM byte denoiser unavailable")
	}
	for trial := range 64 {
		ms, as, ss := 16+trial%5, 16+(trial/5)%7, 16+(trial/7)%9
		mc, sig := make([]byte, 15*ms+16), make([]byte, 15*ss+16)
		for i := range mc {
			mc[i] = byte(i*37 + trial*13)
		}
		for i := range sig {
			sig[i] = byte(i*19 + trial*31)
		}
		align := trial % 8
		size := 15*as + 16
		for inc := range 2 {
			for _, level := range []int{3, 4, 5} {
				got, want := bytes.Repeat([]byte{71}, align+size+13), bytes.Repeat([]byte{71}, align+size+13)
				expected := DenoiseFirstScalar(mc, ms, want[align:align+size], as, sig, ss, 16, inc, level)
				actual := DenoiseFirst(mc, ms, got[align:align+size], as, sig, ss, 16, inc, level)
				if actual != expected || !bytes.Equal(got, want) {
					t.Fatalf("public trial%d inc%d level%d", trial, inc, level)
				}
				for i := range got {
					got[i] = 71
				}
				actual, ok := denoiseFirst16Bytes(mc, ms, got[align:align+size], as, sig, ss, inc, level)
				if !ok || actual != expected || !bytes.Equal(got, want) {
					t.Fatalf("selected trial%d inc%d level%d", trial, inc, level)
				}
			}
		}
	}
}

func TestDenoiseBytesShortDestAndSignal(t *testing.T) {
	for short := range 3 {
		for _, size := range []int{0, 1, 15, 16, 254, 255} {
			var bufs, before [3][]byte
			for i := range bufs {
				n := 256
				if i == short {
					n = size
				}
				bufs[i] = bytes.Repeat([]byte{71}, n)
				before[i] = bytes.Clone(bufs[i])
			}
			if _, ok := denoiseFirst16Bytes(bufs[0], 16, bufs[1], 16, bufs[2], 16, 1, 5); ok {
				t.Fatalf("short%d size%d accepted", short, size)
			}
			for i := range bufs {
				if !bytes.Equal(bufs[i], before[i]) {
					t.Fatalf("short%d size%d buffer%d changed", short, size, i)
				}
			}
		}
	}
}

func TestDenoiseBytesFallbackParameters(t *testing.T) {
	for _, width := range []int{8, 16} {
		for _, parameters := range [][2]int{{-1, 3}, {2, 3}, {0, 2}, {0, 6}, {1, 6}, {0, 0}, {0, 8}} {
			mc, sig := make([]byte, width*width), make([]byte, width*width)
			for i := range mc {
				mc[i] = byte(i*37 + 11)
				sig[i] = byte(i*19 + 53)
			}
			avg, want := make([]byte, width*width), make([]byte, width*width)
			inc, level := parameters[0], parameters[1]
			expected := DenoiseFirstScalar(mc, width, want, width, sig, width, width, inc, level)
			if got := DenoiseFirst(mc, width, avg, width, sig, width, width, inc, level); got != expected || !bytes.Equal(avg, want) {
				t.Fatalf("width%d inc%d level%d sum%d/%d", width, inc, level, got, expected)
			}
		}
	}
}
