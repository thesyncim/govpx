//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestLoopFilterColumns16ExactRows(t *testing.T) {
	for _, pitch := range []int{8, 9, 16, 31, 32, 65, 131} {
		for align := range 16 {
			for pattern := range 10 {
				p := make([]byte, align+15*pitch+8)
				seed := uint32(0x531728 + pattern)
				for i := range p {
					seed = seed*1664525 + 1013904223
					p[i] = byte(seed >> 24)
				}
				a, b, c, d, e, f, g, h := loadLFColumns16(p, align, pitch)
				var cols [8][16]byte
				a.StoreArray(&cols[0])
				b.StoreArray(&cols[1])
				c.StoreArray(&cols[2])
				d.StoreArray(&cols[3])
				e.StoreArray(&cols[4])
				f.StoreArray(&cols[5])
				g.StoreArray(&cols[6])
				h.StoreArray(&cols[7])
				for x := range 8 {
					for y := range 16 {
						if cols[x][y] != p[align+y*pitch+x] {
							t.Fatalf("pitch%d align%d column%d row%d got%d want%d", pitch, align, x, y, cols[x][y], p[align+y*pitch+x])
						}
					}
				}
			}
		}
	}
}

// The input transpose is inlined by gensimd into the fused macro filter. Check
// all columns independently, including the upper half retained for later edges.
func TestLoopFilterWide20RetainedColumns(t *testing.T) {
	for _, pitch := range []int{20, 21, 31, 32, 65, 1312} {
		for align := range 16 {
			for pattern := range 12 {
				const guard = 23
				span := 15*pitch + 20
				backing := bytes.Repeat([]byte{0xa7}, align+span+guard)
				seed := uint32(0x3514c1 + pattern)
				for y := range 16 {
					for x := range 20 {
						seed = seed*1664525 + 1013904223
						v := byte(seed >> 24)
						if pattern == 0 {
							v = byte(16*y + x)
						}
						backing[align+y*pitch+x] = v
					}
				}
				before := bytes.Clone(backing)
				view := backing[align : align+span : align+span]
				a, b, c, d, e, f, g, h, i, j, k, l, m, n, o, p := loadLFColumnsWide20(view, pitch)
				var cols [16][16]byte
				a.StoreArray(&cols[0])
				b.StoreArray(&cols[1])
				c.StoreArray(&cols[2])
				d.StoreArray(&cols[3])
				e.StoreArray(&cols[4])
				f.StoreArray(&cols[5])
				g.StoreArray(&cols[6])
				h.StoreArray(&cols[7])
				i.StoreArray(&cols[8])
				j.StoreArray(&cols[9])
				k.StoreArray(&cols[10])
				l.StoreArray(&cols[11])
				m.StoreArray(&cols[12])
				n.StoreArray(&cols[13])
				o.StoreArray(&cols[14])
				p.StoreArray(&cols[15])
				for x := range 16 {
					for y := range 16 {
						if got, want := cols[x][y], before[align+y*pitch+x]; got != want {
							t.Fatalf("pitch%d align%d pattern%d column%d row%d got%d want%d", pitch, align, pattern, x, y, got, want)
						}
					}
				}
				if !bytes.Equal(backing, before) {
					t.Fatal("input transpose changed backing storage")
				}
			}
		}
	}
}

func TestMacroAxisCompleteWindows(t *testing.T) {
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{16, 17, 19, 20, 21, 32, 1312} {
			step, outer := pitch, 1
			if vertical {
				step, outer = 1, pitch
			}
			span := 19*step + 15*outer + 1
			for align := range 8 {
				for _, cut := range []int{-1, 0, 1} {
					for _, clips := range []bool{false, true} {
						for _, thresholds := range [][4]byte{{0, 0, 0, 0}, {64, 32, 9, 0}, {254, 254, 255, 255}, {255, 32, 9, 0}, {64, 255, 9, 0}} {
							n := span + cut
							got := bytes.Repeat([]byte{0xa7}, align+n+23)
							for i := range n {
								got[align+i] = byte(112 + (i*7+i/17)%23)
							}
							want := bytes.Clone(got)
							gs, ws := got[align:align+n], want[align:align+n]
							if clips {
								gs, ws = gs[:n:n], ws[:n:n]
							}
							mb, bl, lim, thr := thresholds[0], thresholds[1], thresholds[2], thresholds[3]
							ref := LoopFilterMacroInternal16(ws, step, outer, int(mb), int(bl), int(lim), int(thr))
							var ok bool
							if vertical {
								ok = CanLoopFilterMacroVertical16(gs, pitch, mb, bl)
								if ok {
									LoopFilterMacroVertical16Unchecked(gs, pitch, mb, bl, lim, thr)
								}
							} else {
								ok = CanLoopFilterMacroHorizontal16(gs, pitch, mb, bl)
								if ok {
									LoopFilterMacroHorizontal16Unchecked(gs, pitch, mb, bl, lim, thr)
								}
							}
							if ref != ok || !bytes.Equal(got, want) {
								t.Fatalf("vertical%v pitch%d align%d cut%d clipped%v thresholds%v", vertical, pitch, align, cut, clips, thresholds)
							}
						}
					}
				}
			}
		}
	}
}
func TestMacroAxisInvalidNoWrites(t *testing.T) {
	for _, pitch := range []int{-1, 0, 1, 15, 19, 1<<30 + 32, int(^uint(0) >> 1), -int(^uint(0)>>1) - 1} {
		for _, vertical := range []bool{false, true} {
			p := bytes.Repeat([]byte{123}, 600)
			before := bytes.Clone(p)
			var ok bool
			if vertical {
				ok = CanLoopFilterMacroVertical16(p[:1:1], pitch, 64, 32)
				if ok {
					LoopFilterMacroVertical16Unchecked(p[:1:1], pitch, 64, 32, 9, 0)
				}
			} else {
				ok = CanLoopFilterMacroHorizontal16(p[:1:1], pitch, 64, 32)
				if ok {
					LoopFilterMacroHorizontal16Unchecked(p[:1:1], pitch, 64, 32, 9, 0)
				}
			}
			if ok || !bytes.Equal(p, before) {
				t.Fatalf("invalid pitch%d vertical%v wrote", pitch, vertical)
			}
		}
	}
}

func TestMacroInternalFusionOracle(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	seed := uint32(0x73101)
	next := func() byte { seed = seed*1664525 + 1013904223; return byte(seed >> 24) }
	for _, vertical := range []bool{false, true} {
		for _, extra := range []int{0, 1, 7, 31, 61} {
			step, outer := 16+extra, 1
			if vertical {
				step, outer = 1, 20+extra
			}
			for align := range 8 {
				for trial := range 18 {
					p := make([]byte, align+19*step+15*outer+1+13)
					for i := range p {
						p[i] = next()
						if trial%3 == 0 {
							p[i] = 120 + p[i]%9
						}
					}
					for _, limits := range [][4]int{{0, 0, 0, 0}, {1, 1, 1, 0}, {32, 17, 9, 0}, {64, 33, 32, 8}, {254, 254, 255, 255}} {
						got, want := bytes.Clone(p), bytes.Clone(p)
						mb, bl, lim, thr := limits[0], limits[1], limits[2], limits[3]
						if !LoopFilterMacroInternal16(got[align:len(got)-13], step, outer, mb, bl, lim, thr) {
							t.Fatal("eligible fusion declined")
						}
						for _, edge := range []int{4, 8, 12, 16} {
							kind, b := 4, bl
							if edge == 4 {
								kind, b = 6, mb
							}
							for lane := 0; lane < 16; lane += 8 {
								loopFilterNarrow8(want, align+edge*step+lane*outer, step, outer, b, lim, thr, kind)
							}
						}
						if !bytes.Equal(got, want) {
							t.Fatalf("vertical%v extra%d align%d trial%d limits%v", vertical, extra, align, trial, limits)
						}
					}
				}
			}
		}
	}
	for _, shape := range [][2]int{{0, 1}, {1, 0}, {1, 19}, {15, 1}, {int(^uint(0) >> 1), 1}, {1, int(^uint(0) >> 1)}} {
		p := bytes.Repeat([]byte{123}, 512)
		before := bytes.Clone(p)
		if LoopFilterMacroInternal16(p, shape[0], shape[1], 64, 32, 9, 0) {
			t.Fatal("invalid geometry accepted")
		}
		if !bytes.Equal(p, before) {
			t.Fatal("declined path wrote")
		}
	}
}

// A declined fast path must preserve the entire allocation so callers can
// safely retry the ordinary edge implementation.
func TestMacroInternalDeclinesWithoutStores(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	for _, shape := range [][2]int{{16, 1}, {17, 1}, {1, 20}, {1, 21}, {maxInt, 1}, {1, maxInt}, {-1, 1}, {1, -1}} {
		step, outer := shape[0], shape[1]
		for _, thresholds := range [][4]int{{-1, 32, 9, 0}, {255, 32, 9, 0}, {32, -1, 9, 0}, {32, 255, 9, 0}, {32, 32, -1, 0}, {32, 32, 256, 0}, {32, 32, 9, -1}, {32, 32, 9, 256}, {32, 32, 9, 0}} {
			for _, size := range []int{0, 1, 318} {
				p := bytes.Repeat([]byte{127}, size+32)
				before := bytes.Clone(p)
				if LoopFilterMacroInternal16(p[16:16+size], step, outer, thresholds[0], thresholds[1], thresholds[2], thresholds[3]) {
					t.Fatalf("accepted shape=%v thresholds=%v size=%d", shape, thresholds, size)
				}
				if !bytes.Equal(p, before) {
					t.Fatal("declined path changed its allocation")
				}
			}
		}
	}
}

func TestMacroInternalZeroCorrectionsAndLaterEdges(t *testing.T) {
	if !Enabled {
		t.Fatal("ARM SIMD backend unavailable")
	}
	for _, vertical := range []bool{false, true} {
		for _, pitch := range []int{20, 21, 32, 1312} {
			step, outer := pitch, 1
			if vertical {
				step, outer = 1, pitch
			}
			for align := range 8 {
				for pattern := range 10 {
					p := bytes.Repeat([]byte{0xa7}, align+19*step+15*outer+1+13)
					for axis := range 20 {
						for lane := range 16 {
							v := byte([]int{0, 127, 128, 255, 0, 0, 0, 0, 0, 0}[pattern])
							if pattern >= 6 {
								delta := axis / 4
								if pattern >= 8 {
									delta *= 2
								}
								if pattern%2 != 0 {
									delta = -delta
								}
								v = byte(128 + delta)
							} else if pattern >= 4 {
								// A linear ramp with HEV threshold zero has cancelling
								// outer and central terms at all four edges.
								v = byte(32 + lane*3 + axis)
								if pattern == 5 && axis == 16 && lane == 15 {
									v += 6 // Only the last edge/lane needs a correction.
								}
							}
							p[align+axis*step+lane*outer] = v
						}
					}
					got, want := bytes.Clone(p), bytes.Clone(p)
					if !LoopFilterMacroInternal16(got[align:len(got)-13], step, outer, 254, 254, 255, 0) {
						t.Fatal("eligible zero-correction fixture declined")
					}
					for _, edge := range []int{4, 8, 12, 16} {
						kind := 4
						if edge == 4 {
							kind = 6
						}
						for lane := 0; lane < 16; lane += 8 {
							loopFilterNarrow8(want, align+edge*step+lane*outer, step, outer, 254, 255, 0, kind)
						}
					}
					if !bytes.Equal(got, want) || bytes.Equal(want, p) != (pattern != 5 && pattern < 8) {
						t.Fatalf("vertical=%v pitch=%d align=%d pattern=%d", vertical, pitch, align, pattern)
					}
				}
			}
		}
	}
}
