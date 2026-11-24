package pixel

import "testing"

func BenchmarkLoopFilterFlatPackets(b *testing.B) {
	for _, kind := range []int{8, 16} {
		name := "8"
		if kind == 16 {
			name = "16"
		}
		for _, vertical := range []bool{false, true} {
			dir := "H"
			if vertical {
				dir = "V"
			}
			b.Run(dir+name, func(b *testing.B) {
				p := make([]byte, 32*32)
				for i := range p {
					p[i] = 128
				}
				step, outer := 32, 1
				if vertical {
					step, outer = 1, 32
				}
				b.ReportAllocs()
				for b.Loop() {
					LoopFilter(p, 8*step, step, outer, 8, 64, 32, 8, kind)
				}
			})
		}
	}
}
