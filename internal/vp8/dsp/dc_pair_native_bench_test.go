package dsp

import (
	"bytes"
	"fmt"
	"testing"
)

var dcPairNativeSink byte

func BenchmarkDCPairRowsNative(b *testing.B) {
	for _, pitch := range []int{8, 32, 1312} {
		for _, inplace := range []bool{false, true} {
			for pair, dc := range [][2]int32{{87, -99}, {2044, -2044}, {0, 0}} {
				b.Run(fmt.Sprintf("pitch%d/inplace%v/pair%d", pitch, inplace, pair), func(b *testing.B) {
					ss, ds := pitch, pitch+5
					if inplace {
						ds = ss
					}
					input := make([]byte, 3+3*ss+8+13)
					for i := range input {
						input[i] = byte(i*37 + 11)
					}
					output := bytes.Repeat([]byte{0xa7}, 5+3*ds+8+17)
					pred, dst := input[3:3+3*ss+8], output[5:5+3*ds+8]
					if inplace {
						output, dst = input, pred
					}
					original := bytes.Clone(input)
					want := bytes.Clone(output)
					start := 5
					if inplace {
						start = 3
					}
					for y := range 4 {
						for x := range 8 {
							delta := int((dc[x/4] + 4) >> 3)
							delta = min(255, max(-255, delta))
							want[start+y*ds+x] = byte(min(255, max(0, int(pred[y*ss+x])+delta)))
						}
					}
					fn := DCOnlyIDCT4x4AddPairInt32
					fn(dc[0], dc[1], pred, ss, dst, ds)
					if !bytes.Equal(output, want) {
						b.Fatal("DC pair differs from independent clipped scalar equation")
					}
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						if inplace {
							// Restore only the 32 pixels used by the public operation.
							copy(pred[:8], original[3:11])
							copy(pred[ss:ss+8], original[3+ss:11+ss])
							copy(pred[2*ss:2*ss+8], original[3+2*ss:11+2*ss])
							copy(pred[3*ss:3*ss+8], original[3+3*ss:11+3*ss])
						}
						fn(dc[0], dc[1], pred, ss, dst, ds)
					}
					if !bytes.Equal(output, want) || !inplace && !bytes.Equal(input, original) {
						b.Fatal("final output, immutable input or backing canary changed")
					}
					dcPairNativeSink ^= dst[0] ^ dst[3*ds+7]
				})
			}
		}
	}
}
