//go:build goexperiment.simd && arm64 && !purego

package pixel

import "testing"

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
