package encoder

import (
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func BenchmarkVP8WorkElisionFDCT(b *testing.B) {
	for _, pattern := range []string{"zero", "dc", "alternating", "sparse", "dense"} {
		b.Run(pattern, func(b *testing.B) {
			var in, out [256]int16
			for i := range in {
				switch pattern {
				case "dc":
					in[i] = [7]int16{-255, -128, -1, 0, 1, 128, 255}[(i/16)%7]
				case "alternating":
					if (i/16)%2 != 0 {
						in[i] = int16((i*37+11)%511 - 255)
					}
				case "sparse":
					if i%32 == 0 {
						in[i] = 17
					}
				case "dense":
					in[i] = int16((i*37+11)%511 - 255)
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				ForwardDCT4x4Batch(in[:], out[:], 16)
			}
		})
	}
}
func TestVP8WorkElisionFDCTPackets(t *testing.T) {
	if !pixel.Enabled {
		t.Skip("SIMD packet order")
	}
	for _, count := range []int{1, 2, 3, 8, 16, 25} {
		for delta := -17; delta <= 17; delta++ {
			for pattern := 0; pattern < 5; pattern++ {
				const off = 64
				got := make([]int16, off+25*16+80)
				for i := range got {
					got[i] = 0x1357
				}
				for i := 0; i < count*16; i++ {
					switch pattern {
					case 0:
						got[off+i] = 0
					case 1:
						got[off+i] = [7]int16{-255, -128, -1, 0, 1, 128, 255}[(i/16)%7]
					case 2:
						if (i/16)%2 != 0 {
							got[off+i] = int16((i*37+11)%511 - 255)
						} else {
							got[off+i] = 0
						}
					case 3:
						got[off+i] = int16((i*37+11)%511 - 255)
					case 4:
						got[off+i] = int16(i * 17557)
					}
				}
				want := append([]int16(nil), got...)
				in, out := want[off:off+count*16], want[off+delta:off+delta+count*16]
				i := 0
				for ; i+2 <= count; i += 2 {
					var stage, result [32]int16
					copy(stage[:], in[i*16:i*16+32])
					peak := 0
					for _, v := range stage {
						x := int(v)
						if x < 0 {
							x = -x
						}
						if x > peak {
							peak = x
						}
					}
					if peak > 255 {
						forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
						forwardDCT4x4Scalar(in[i*16+16:], 4, (*[16]int16)(out[i*16+16:]))
					} else {
						forwardDCT4x4Scalar(stage[:16], 4, (*[16]int16)(result[:16]))
						forwardDCT4x4Scalar(stage[16:], 4, (*[16]int16)(result[16:]))
						copy(out[i*16:i*16+32], result[:])
					}
				}
				for ; i < count; i++ {
					forwardDCT4x4Scalar(in[i*16:], 4, (*[16]int16)(out[i*16:]))
				}
				ForwardDCT4x4Batch(got[off:off+count*16], got[off+delta:off+delta+count*16], count)
				for lane, x := range got {
					if x != want[lane] {
						t.Fatalf("count%d delta%d pattern%d lane%d got%d want%d", count, delta, pattern, lane, x, want[lane])
					}
				}
			}
		}
	}
}

func BenchmarkVP8WorkElisionSingle(b *testing.B) {
	for _, walsh := range []bool{false, true} {
		for _, pat := range []string{"zero", "dc", "dense"} {
			name := "dct"
			if walsh {
				name = "walsh"
			}
			b.Run(name+"/"+pat, func(b *testing.B) {
				var in, out [16]int16
				for i := range in {
					if pat == "dc" {
						in[i] = 17
					} else if pat == "dense" {
						in[i] = int16((i*37+11)%511 - 255)
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					if walsh {
						ForwardWalsh4x4(in[:], 4, &out)
					} else {
						ForwardDCT4x4(in[:], 4, &out)
					}
				}
			})
		}
	}
}
func BenchmarkVP8WorkElisionGather(b *testing.B) {
	for _, edge := range []bool{false, true} {
		name := "interior"
		width := 48
		height := 48
		x := 16
		y := 16
		if edge {
			name = "edge"
			x = 40
			y = 40
		}
		b.Run(name, func(b *testing.B) {
			src := make([]byte, 48*48)
			pred := make([]byte, 16*16)
			for i := range src {
				src[i] = byte(i * 17)
			}
			for i := range pred {
				pred[i] = byte(i * 31)
			}
			var out [256]int16
			b.ReportAllocs()
			for b.Loop() {
				GatherMacroblockYResiduals4x4FromPredBuffer(src, 48, width, height, pred, 16, x, y, out[:])
			}
		})
	}
}
func BenchmarkVP8WorkElisionBPred(b *testing.B) {
	src := SourceImage{Y: make([]byte, 48*48), YStride: 48, Width: 48, Height: 48}
	for i := range src.Y {
		src.Y[i] = byte(i * 17)
	}
	pred := make([]byte, 16*16)
	for i := range pred {
		pred[i] = byte(i * 31)
	}
	b.Run("sse", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			vp8WorkElisionSink = BPredBlockSSE(src, 1, 1, 5, pred, 4)
		}
	})
	b.Run("residual", func(b *testing.B) {
		var out [16]int16
		b.ReportAllocs()
		for b.Loop() {
			FillBPredResidual4x4(src, 1, 1, 5, pred, &out)
		}
	})
	b.Run("predicted", func(b *testing.B) {
		var out [16]int16
		b.ReportAllocs()
		for b.Loop() {
			FillPredictedResidual4x4(src.Y, 48, 48, 48, src.Y, 48, 20, 20, &out)
		}
	})
}

var vp8WorkElisionSink int

func TestVP8WorkElisionResidualBridges(t *testing.T) {
	for _, w := range []int{1, 15, 16, 17, 48} {
		for _, h := range []int{1, 15, 16, 19, 48} {
			stride := w + 5
			src := make([]byte, stride*h)
			pred := make([]byte, 256)
			for i := range src {
				src[i] = byte(i * 17)
			}
			for i := range pred {
				pred[i] = byte(i * 31)
			}
			for _, x := range []int{0, w - 1} {
				for _, y := range []int{0, h - 1} {
					var got, want [256]int16
					GatherMacroblockYResiduals4x4FromPredBuffer(src, stride, w, h, pred, 16, x, y, got[:])
					for block := range 16 {
						bx := (block & 3) * 4
						by := (block >> 2) * 4
						for r := range 4 {
							for c := range 4 {
								sx := min(x+bx+c, w-1)
								sy := min(y+by+r, h-1)
								want[block*16+r*4+c] = int16(src[sy*stride+sx]) - int16(pred[(by+r)*16+bx+c])
							}
						}
					}
					if got != want {
						t.Fatalf("gather %dx%d@%d,%d", w, h, x, y)
					}
				}
			}
			image := SourceImage{Y: src, YStride: stride, Width: w, Height: h}
			for block := range 16 {
				var got, want [16]int16
				FillBPredResidual4x4(image, 0, 0, block, pred, &got)
				sse := 0
				for r := range 4 {
					for c := range 4 {
						x := min((block&3)*4+c, w-1)
						y := min((block>>2)*4+r, h-1)
						v := int(src[y*stride+x]) - int(pred[r*4+c])
						want[r*4+c] = int16(v)
						sse += v * v
					}
				}
				if got != want || BPredBlockSSE(image, 0, 0, block, pred, 4) != sse {
					t.Fatalf("bpred %dx%d block%d", w, h, block)
				}
			}
		}
	}
}

func BenchmarkVP8WorkElisionQuant(b *testing.B) {
	for _, pat := range []string{"zero", "below", "sparse", "dense"} {
		for _, count := range []int{1, 2, 8, 16} {
			b.Run(fmt.Sprintf("%s/blocks%d", pat, count), func(b *testing.B) {
				var deq [16]int16
				for i := range deq {
					deq[i] = int16(i + 10)
				}
				var quant BlockQuant
				InitFastBlockQuant(&deq, &quant)
				c := make([]int16, count*16)
				q := make([]int16, len(c))
				dq := make([]int16, len(c))
				e := make([]uint8, count)
				for i := range c {
					if pat == "below" {
						c[i] = 1
					} else if pat == "below" {
						c[i] = 1
					} else if pat == "dense" {
						c[i] = int16((i*17)%8191 - 4095)
					} else if pat == "sparse" && i%16 == 0 {
						c[i] = 17
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					FastQuantizeBlockBatch(c, &quant, q, dq, e, count)
				}
			})
		}
	}
}
