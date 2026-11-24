package encoder

import "testing"

func BenchmarkForwardDCT32x32Native(b *testing.B) {
	var in [1024]int16
	for i := range in {
		in[i] = int16((i*37+11)%511 - 255)
	}
	for _, rd := range []bool{false, true} {
		name := "precise"
		if rd {
			name = "rd"
		}
		b.Run(name, func(b *testing.B) {
			var out [1024]int16
			b.ReportAllocs()
			if rd {
				for b.Loop() {
					ForwardDCT32x32RDInto(in[:], 32, out[:])
				}
			} else {
				for b.Loop() {
					ForwardDCT32x32Into(in[:], 32, out[:])
				}
			}
		})
	}
}
