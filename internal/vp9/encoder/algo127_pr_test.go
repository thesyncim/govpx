package encoder

import (
	"fmt"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

func algo127Round3Nmv(seed int) vp9dec.NmvContext {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	for a := range fc.Nmvc.Comps {
		c := &fc.Nmvc.Comps[a]
		c.Sign = uint8(1 + (seed+77*a)%255)
		c.Hp = uint8(1 + (seed+19*a)%255)
		c.Class0Hp = uint8(1 + (seed+31*a)%255)
		for i := range c.Bits {
			c.Bits[i] = uint8(1 + (seed+11*i+43*a)%255)
		}
		for i := range c.Classes {
			c.Classes[i] = uint8(1 + (seed+37*i+9*a)%255)
		}
		for i := range c.Fp {
			c.Fp[i] = uint8(1 + (seed+53*i+17*a)%255)
		}
	}
	return fc.Nmvc
}

func TestAlgo127Round3Nmv(t *testing.T) {
	for _, seed := range []int{0, 1, 17, 128, 254} {
		for _, hp := range []bool{false, true} {
			ctx := algo127Round3Nmv(seed)
			for a := range ctx.Comps {
				var got, want [nmvCostTableVals]int
				buildNmvComponentCostTable(got[:], &ctx.Comps[a], hp)
				algo127Round3FrozenNmv(want[:], &ctx.Comps[a], hp)
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("seed%d hp%v axis%d value%d got%d want%d", seed, hp, a, i, got[i], want[i])
					}
				}
			}
		}
	}
}

var algo127Round3NmvTable NmvCostTable

func BenchmarkAlgo127Round3Nmv(b *testing.B) {
	for _, hp := range []bool{false, true} {
		b.Run(fmt.Sprint(hp), func(b *testing.B) {
			ctx := algo127Round3Nmv(73)
			b.ReportAllocs()
			for b.Loop() {
				algo127Round3NmvTable.Build(&ctx, hp)
			}
		})
	}
}

func algo127Round3FrozenNmv(mvcost []int, c *vp9dec.NmvComponent, useHP bool) {
	if len(mvcost) < nmvCostTableVals || c == nil {
		return
	}
	offset := nmvCostTableMax
	signCost := [2]int{VP9CostBit(c.Sign, 0), VP9CostBit(c.Sign, 1)}
	var classCost [vp9dec.MvClasses]int
	VP9CostTokens(classCost[:], c.Classes[:], tables.MvClassTree[:])
	var class0Cost [vp9dec.Class0Size]int
	VP9CostTokens(class0Cost[:], c.Class0[:], tables.MvClass0Tree[:])
	var bitsCost [vp9dec.MvOffsetBits][2]int
	for i := range bitsCost {
		bitsCost[i][0] = VP9CostBit(c.Bits[i], 0)
		bitsCost[i][1] = VP9CostBit(c.Bits[i], 1)
	}
	var class0FPCost [vp9dec.Class0Size][vp9dec.MvFpSize]int
	for i := range class0FPCost {
		VP9CostTokens(class0FPCost[i][:], c.Class0Fp[i][:], tables.MvFpTree[:])
	}
	var fpCost [vp9dec.MvFpSize]int
	VP9CostTokens(fpCost[:], c.Fp[:], tables.MvFpTree[:])
	class0HPCost := [2]int{VP9CostBit(c.Class0Hp, 0), VP9CostBit(c.Class0Hp, 1)}
	hpCost := [2]int{VP9CostBit(c.Hp, 0), VP9CostBit(c.Hp, 1)}

	mvcost[offset] = 0
	for o := range vp9dec.Class0Size << 3 {
		d := o >> 3
		f := (o >> 1) & 3
		cost := classCost[tables.MvClass0] + class0Cost[d] + class0FPCost[d][f]
		if useHP {
			cost += class0HPCost[o&1]
		}
		v := o + 1
		mvcost[offset+v] = cost + signCost[0]
		mvcost[offset-v] = cost + signCost[1]
	}
	for cls := tables.MvClass1; cls < vp9dec.MvClasses; cls++ {
		for d := 0; d < (1 << uint(cls)); d++ {
			wholeCost := classCost[cls]
			n := cls + vp9dec.Class0Bits - 1
			for i := range n {
				wholeCost += bitsCost[i][(d>>uint(i))&1]
			}
			for f := range vp9dec.MvFpSize {
				cost := wholeCost + fpCost[f]
				v := (vp9dec.Class0Size << uint(cls+2)) + d*8 + f*2 + 1
				if useHP {
					mvcost[offset+v] = cost + hpCost[0] + signCost[0]
					mvcost[offset-v] = cost + hpCost[0] + signCost[1]
					if v+1 > nmvCostTableMax {
						break
					}
					mvcost[offset+v+1] = cost + hpCost[1] + signCost[0]
					mvcost[offset-v-1] = cost + hpCost[1] + signCost[1]
					continue
				}
				mvcost[offset+v] = cost + signCost[0]
				mvcost[offset-v] = cost + signCost[1]
				if v+1 > nmvCostTableMax {
					break
				}
				mvcost[offset+v+1] = cost + signCost[0]
				mvcost[offset-v-1] = cost + signCost[1]
			}
		}
	}
}

// Frozen from production870699ce; candidate source never generates expectations.
