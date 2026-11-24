//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

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
