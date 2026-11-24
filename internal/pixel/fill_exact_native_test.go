package pixel

import (
	"bytes"
	"fmt"
	"testing"
)

func fillPriorOracle(dst []byte, ds, w, h int, value byte) {
	if !Enabled {
		for y := range h {
			for x := range w {
				dst[y*ds+x] = value
			}
		}
		return
	}
	for y := range h {
		row := dst[y*ds : y*ds+w]
		for x := range min(len(row), 16) {
			row[x] = value
		}
	}
}
func fillPanic(f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	f()
	return false
}
func TestFillExactPublicState(t *testing.T) {
	for _, w := range []int{-1, 0, 1, 3, 4, 5, 8, 15, 16, 17, 32} {
		for _, h := range []int{-1, 0, 1, 2, 4, 8, 16, 32, 64, 65} {
			for _, ds := range []int{-1, 0, 1, 3, 4, 7, 8, 15, 16, 17, 32, 67} {
				for _, align := range []int{0, 1, 3, 7, 15} {
					for _, visible := range []int{0, 1, 3, 7, 15, 16, 31, 127, 1023, 4352} {
						a, b := make([]byte, 4400), make([]byte, 4400)
						for i := range a {
							a[i] = byte(i*37 + 91)
						}
						copy(b, a)
						got := fillPanic(func() { FillBlock(a[align:align+visible], ds, w, h, 213) })
						want := fillPanic(func() { fillPriorOracle(b[align:align+visible], ds, w, h, 213) })
						if got != want || !bytes.Equal(a, b) {
							t.Fatalf("w%d h%d ds%d align%d visible%d panic%v/%v", w, h, ds, align, visible, got, want)
						}
					}
				}
			}
		}
	}
}

var fillSink byte

func BenchmarkFillExactPublic(b *testing.B) {
	for _, w := range []int{4, 8, 16} {
		for _, h := range []int{1, 8, 16} {
			for _, ds := range []int{w, 37, 1312} {
				b.Run(fmt.Sprintf("w%d/h%d/pitch%d", w, h, ds), func(b *testing.B) {
					storage := make([]byte, (h-1)*ds+w+32)
					for i := range storage {
						storage[i] = byte(i*29 + 3)
					}
					dst := storage[7 : 7+(h-1)*ds+w : 7+(h-1)*ds+w]
					expected := append([]byte(nil), storage...)
					value := byte(11)
					b.ReportAllocs()
					for b.Loop() {
						value++
						FillBlock(dst, ds, w, h, value)
					}
					for y := range h {
						for x := range w {
							expected[7+y*ds+x] = value
						}
					}
					if !bytes.Equal(storage, expected) {
						b.Fatal("complete output/canary mismatch")
					}
					fillSink = dst[len(dst)-1]
				})
			}
		}
	}
}

func TestFillExactCapacityAndValues(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, w := range []int{4, 8, 16} {
		for _, h := range []int{0, 1, 2, 3, 4, 7, 8, 16, 63, 64, 65} {
			for _, ds := range []int{-1, 0, 1, w - 1, w, w + 3, 37, 1312, (1 << 30) - 1, 1 << 30, maxInt, -maxInt - 1} {
				for _, align := range []int{0, 1, 7, 15} {
					for _, value := range []byte{0, 1, 127, 128, 213, 254, 255} {
						need := w
						if h > 0 && ds >= 0 && ds < 1313 {
							need = (h-1)*ds + w
						}
						need = min(need, 8192)
						windows := [][2]int{{0, 0}, {0, 8192}, {max(0, need-1), max(0, need-1)}, {max(0, need-1), need}, {need, need}, {need, min(8192, need+17)}, {w, min(8192, w+max(ds, 0))}}
						for _, win := range windows {
							visible, capacity := win[0], win[1]
							if capacity < visible || capacity > 8192 {
								continue
							}
							a, b := make([]byte, align+8192+19), make([]byte, align+8192+19)
							for i := range a {
								a[i] = byte(i*37 + 91)
							}
							copy(b, a)
							got := fillPanic(func() { FillBlock(a[align:align+visible:align+capacity], ds, w, h, value) })
							want := fillPanic(func() { fillPriorOracle(b[align:align+visible:align+capacity], ds, w, h, value) })
							if got != want || !bytes.Equal(a, b) {
								t.Fatalf("w%d h%d ds%d align%d len%d cap%d value%d panic%v/%v", w, h, ds, align, visible, capacity, value, got, want)
							}
						}
					}
				}
			}
		}
	}
	// Every payload, exact complete capacity, overlapping and disjoint rows.
	for value := range 256 {
		for _, w := range []int{4, 8, 16} {
			for _, ds := range []int{0, 1, w, w + 3} {
				size := ds + w
				a, b := make([]byte, size+2), make([]byte, size+2)
				for i := range a {
					a[i] = 19
				}
				copy(b, a)
				FillBlock(a[1:1+size:1+size], ds, w, 2, byte(value))
				fillPriorOracle(b[1:1+size:1+size], ds, w, 2, byte(value))
				if !bytes.Equal(a, b) {
					t.Fatalf("payload%d w%d ds%d", value, w, ds)
				}
			}
		}
	}
}

func BenchmarkFillExactFinal(b *testing.B) {
	for _, w := range []int{4, 8, 16} {
		for _, h := range []int{1, w} {
			for _, ds := range []int{w, 37, 1312} {
				b.Run(fmt.Sprintf("w%d/h%d/pitch%d", w, h, ds), func(b *testing.B) {
					storage := make([]byte, (h-1)*ds+w+32)
					for i := range storage {
						storage[i] = byte(i*29 + 3)
					}
					dst := storage[7 : 7+(h-1)*ds+w : 7+(h-1)*ds+w]
					expected := append([]byte(nil), storage...)
					value := byte(11)
					b.ReportAllocs()
					for b.Loop() {
						value++
						FillBlock(dst, ds, w, h, value)
					}
					for y := range h {
						for x := range w {
							expected[7+y*ds+x] = value
						}
					}
					if !bytes.Equal(storage, expected) {
						b.Fatal("complete output/canary mismatch")
					}
					fillSink = dst[len(dst)-1]
				})
			}
		}
	}
}
