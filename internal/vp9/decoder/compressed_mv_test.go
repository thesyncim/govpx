package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
)

// TestReadMvProbsNoUpdatesPreservesAll writes a bitstream of "no
// update" bits matching the exact number of probability slots
// read_mv_probs touches in (allow_hp=false / allow_hp=true) mode and
// confirms every slot in the NmvContext keeps its prior value.
func TestReadMvProbsNoUpdatesPreservesAll(t *testing.T) {
	cases := []struct {
		name    string
		allowHp bool
		slots   int
	}{
		// joints: 3
		// per comp (×2): sign(1) + classes(10) + class0(1) + bits(10) = 22
		// per comp (×2): class0_fp(2 ×3) + fp(3) = 9
		// allow_hp: per comp (×2): class0_hp(1) + hp(1) = 2 each
		{
			name:    "no_hp",
			allowHp: false,
			slots:   3 + 2*22 + 2*9, // = 65
		},
		{
			name:    "with_hp",
			allowHp: true,
			slots:   3 + 2*22 + 2*9 + 2*2, // = 69
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := make([]byte, 64)
			var w bitstream.Writer
			w.Start(buf)
			for range c.slots {
				w.Write(0, MvUpdateProb)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}
			var r bitstream.Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}

			var ctx NmvContext
			// Seed every slot so we can detect any unintended write.
			seed := uint8(33)
			ctx.Joints[0] = seed
			ctx.Joints[1] = seed
			ctx.Joints[2] = seed
			for i := range 2 {
				cc := &ctx.Comps[i]
				cc.Sign = seed
				for j := range cc.Classes {
					cc.Classes[j] = seed
				}
				for j := range cc.Class0 {
					cc.Class0[j] = seed
				}
				for j := range cc.Bits {
					cc.Bits[j] = seed
				}
				for j := range cc.Class0Fp {
					for k := range cc.Class0Fp[j] {
						cc.Class0Fp[j][k] = seed
					}
				}
				for j := range cc.Fp {
					cc.Fp[j] = seed
				}
				cc.Class0Hp = seed
				cc.Hp = seed
			}

			ReadMvProbs(&r, &ctx, c.allowHp)

			check := func(name string, got uint8) {
				if got != seed {
					t.Errorf("%s changed: got %d, want %d", name, got, seed)
				}
			}
			for i, v := range ctx.Joints {
				check("Joints", v)
				_ = i
			}
			for i := range 2 {
				cc := &ctx.Comps[i]
				check("Sign", cc.Sign)
				for _, v := range cc.Classes {
					check("Classes", v)
				}
				for _, v := range cc.Class0 {
					check("Class0", v)
				}
				for _, v := range cc.Bits {
					check("Bits", v)
				}
				for _, row := range cc.Class0Fp {
					for _, v := range row {
						check("Class0Fp", v)
					}
				}
				for _, v := range cc.Fp {
					check("Fp", v)
				}
				check("Class0Hp", cc.Class0Hp)
				check("Hp", cc.Hp)
			}
		})
	}
}

func TestNmvComponentLayout(t *testing.T) {
	// Spot-check the constants — these are wire-stable and any drift
	// from libvpx would corrupt every MV update frame-wide.
	if MvJoints != 4 || MvClasses != 11 || Class0Size != 2 || MvOffsetBits != 10 || MvFpSize != 4 {
		t.Fatalf("MV constants wrong: joints=%d classes=%d class0=%d offset=%d fp=%d",
			MvJoints, MvClasses, Class0Size, MvOffsetBits, MvFpSize)
	}
}
