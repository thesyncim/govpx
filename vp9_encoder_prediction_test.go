package govpx

import (
	"bytes"
	"slices"
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func TestVP9BareFPQuantizeMatchesTrellisWrapper(t *testing.T) {
	tests := []struct {
		name          string
		txSize        common.TxSize
		txType        common.TxType
		lossless      bool
		useLp32x32RD  bool
		sfLp32x32Fdct int
	}{
		{name: "tx4_dct", txSize: common.Tx4x4, txType: common.DctDct},
		{name: "tx4_lossless", txSize: common.Tx4x4, txType: common.AdstDct, lossless: true},
		{name: "tx8_adst_dct", txSize: common.Tx8x8, txType: common.AdstDct},
		{name: "tx16_dct_adst", txSize: common.Tx16x16, txType: common.DctAdst},
		{name: "tx32_dct", txSize: common.Tx32x32, txType: common.DctDct},
		{name: "tx32_lp", txSize: common.Tx32x32, txType: common.DctDct, useLp32x32RD: true},
		{name: "tx32_sf_lp", txSize: common.Tx32x32, txType: common.DctDct, sfLp32x32Fdct: 1},
	}
	for _, tc := range tests {
		for _, wantQ := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				maxEob := vp9dec.MaxEobForTxSize(tc.txSize)
				bs := 4 << uint(tc.txSize)
				stride := 32
				dequant := [2]int16{7, 9}
				fpTables := vp9enc.QuantizeFPTablesForDequant(dequant)

				var oldEnc, bareEnc, preEnc VP9Encoder
				oldEnc.sf.UseLp32x32Fdct = tc.sfLp32x32Fdct
				bareEnc.sf.UseLp32x32Fdct = tc.sfLp32x32Fdct
				preEnc.sf.UseLp32x32Fdct = tc.sfLp32x32Fdct
				for y := range bs {
					for x := range bs {
						v := ((x*17 + y*29 + int(tc.txSize)*11) % 47) - 23
						if (x+y)&3 == 0 {
							v *= 3
						}
						oldEnc.residueScratch[y*bs+x] = int16(v)
						bareEnc.residueScratch[y*bs+x] = int16(v)
						preEnc.residueScratch[y*bs+x] = int16(v)
					}
				}

				oldDst := bytes.Repeat([]byte{128}, stride*bs)
				bareDst := slices.Clone(oldDst)
				preDst := slices.Clone(oldDst)
				oldOut := make([]int16, maxEob)
				bareOut := make([]int16, maxEob)
				preOut := make([]int16, maxEob)
				var oldQ, bareQ, preQ []int16
				if wantQ {
					oldQ = make([]int16, maxEob)
					bareQ = make([]int16, maxEob)
					preQ = make([]int16, maxEob)
				}
				scanOrder := &common.ScanOrders[tc.txSize][tc.txType]
				if tc.lossless {
					scanOrder = &common.DefaultScanOrders[tc.txSize]
				}

				oldEOB := oldEnc.quantizeVP9TxResidualWithQTrellisFPTables(oldDst,
					stride, tc.txSize, tc.txType, dequant, 0, oldOut, oldQ,
					tc.lossless, true, tc.useLp32x32RD, fpTables, nil)
				bareEOB := bareEnc.quantizeVP9TxResidualFPWithQTables(bareDst,
					stride, tc.txSize, tc.txType, dequant, bareOut, bareQ,
					tc.lossless, tc.useLp32x32RD, fpTables)
				preEOB := preEnc.quantizeVP9TxResidualFPWithQTablesPrechecked(preDst,
					stride, tc.txSize, tc.txType, maxEob, scanOrder, dequant,
					preOut, preQ, tc.lossless, tc.useLp32x32RD, fpTables)

				if bareEOB != oldEOB {
					t.Fatalf("eob = %d, want %d", bareEOB, oldEOB)
				}
				if preEOB != oldEOB {
					t.Fatalf("prechecked eob = %d, want %d", preEOB, oldEOB)
				}
				if !slices.Equal(bareOut, oldOut) {
					t.Fatalf("dqcoeff mismatch\nbare=%v\nold=%v", bareOut, oldOut)
				}
				if !slices.Equal(preOut, oldOut) {
					t.Fatalf("prechecked dqcoeff mismatch\npre=%v\nold=%v", preOut, oldOut)
				}
				if wantQ && !slices.Equal(bareQ, oldQ) {
					t.Fatalf("qcoeff mismatch\nbare=%v\nold=%v", bareQ, oldQ)
				}
				if wantQ && !slices.Equal(preQ, oldQ) {
					t.Fatalf("prechecked qcoeff mismatch\npre=%v\nold=%v", preQ, oldQ)
				}
				if !bytes.Equal(bareDst, oldDst) {
					t.Fatalf("recon mismatch\nbare=%v\nold=%v", bareDst, oldDst)
				}
				if !bytes.Equal(preDst, oldDst) {
					t.Fatalf("prechecked recon mismatch\npre=%v\nold=%v", preDst, oldDst)
				}
			})
		}
	}
}

func TestVP9InterSkipTxfmACDCLumaGate(t *testing.T) {
	base := func() (*VP9Encoder, *vp9InterEncodeState, vp9InterModeDecision) {
		e := &VP9Encoder{}
		e.sf.UseNonrdPickMode = 1
		e.sf.UseQuantFp = 1
		inter := &vp9InterEncodeState{}
		decision := vp9InterModeDecision{skipTxfm: vp9enc.SkipTxfmAcDc}
		return e, inter, decision
	}
	tests := []struct {
		name string
		edit func(*VP9Encoder, *vp9InterEncodeState, *vp9InterModeDecision) (plane, segID int)
		want bool
	}{
		{
			name: "luma_segment0_realtime_fp",
			edit: func(*VP9Encoder, *vp9InterEncodeState, *vp9InterModeDecision) (int, int) {
				return 0, 0
			},
			want: true,
		},
		{
			name: "chroma_not_skipped",
			edit: func(*VP9Encoder, *vp9InterEncodeState, *vp9InterModeDecision) (int, int) {
				return 1, 0
			},
		},
		{
			name: "segment_nonzero_clears_flag",
			edit: func(*VP9Encoder, *vp9InterEncodeState, *vp9InterModeDecision) (int, int) {
				return 0, 1
			},
		},
		{
			name: "lossless_clears_flag",
			edit: func(_ *VP9Encoder, inter *vp9InterEncodeState, _ *vp9InterModeDecision) (int, int) {
				inter.lossless = true
				return 0, 0
			},
		},
		{
			name: "non_fp_speed_not_consumed",
			edit: func(e *VP9Encoder, _ *vp9InterEncodeState, _ *vp9InterModeDecision) (int, int) {
				e.sf.UseQuantFp = 0
				return 0, 0
			},
		},
		{
			name: "fullrd_path_not_consumed",
			edit: func(e *VP9Encoder, _ *vp9InterEncodeState, _ *vp9InterModeDecision) (int, int) {
				e.sf.UseNonrdPickMode = 0
				return 0, 0
			},
		},
		{
			name: "ac_only_not_consumed",
			edit: func(_ *VP9Encoder, _ *vp9InterEncodeState, decision *vp9InterModeDecision) (int, int) {
				decision.skipTxfm = vp9enc.SkipTxfmAcOnly
				return 0, 0
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, inter, decision := base()
			plane, segID := tc.edit(e, inter, &decision)
			if got := e.vp9InterSkipTxfmACDCLuma(inter, decision, plane, segID); got != tc.want {
				t.Fatalf("vp9InterSkipTxfmACDCLuma = %v, want %v", got, tc.want)
			}
		})
	}
}

var benchmarkVP9BareFPQuantizeEOB int

func BenchmarkVP9BareFPQuantizeCommit(b *testing.B) {
	tests := []struct {
		name   string
		txSize common.TxSize
		txType common.TxType
	}{
		{name: "tx8", txSize: common.Tx8x8, txType: common.DctDct},
		{name: "tx16", txSize: common.Tx16x16, txType: common.DctDct},
	}
	for _, tc := range tests {
		for _, mode := range []string{"wrapper", "bare", "prechecked"} {
			b.Run(tc.name+"/"+mode, func(b *testing.B) {
				maxEob := vp9dec.MaxEobForTxSize(tc.txSize)
				bs := 4 << uint(tc.txSize)
				stride := 32
				dequant := [2]int16{7, 9}
				fpTables := vp9enc.QuantizeFPTablesForDequant(dequant)
				scanOrder := &common.ScanOrders[tc.txSize][tc.txType]
				var e VP9Encoder
				for y := range bs {
					for x := range bs {
						v := ((x*17 + y*29 + int(tc.txSize)*11) % 47) - 23
						if (x+y)&3 == 0 {
							v *= 3
						}
						e.residueScratch[y*bs+x] = int16(v)
					}
				}
				dst := bytes.Repeat([]byte{128}, stride*bs)
				dstInit := slices.Clone(dst)
				out := make([]int16, maxEob)
				qOut := make([]int16, maxEob)

				b.ReportAllocs()
				for b.Loop() {
					copy(dst, dstInit)
					if mode == "bare" {
						benchmarkVP9BareFPQuantizeEOB += e.quantizeVP9TxResidualFPWithQTables(
							dst, stride, tc.txSize, tc.txType, dequant, out, qOut,
							false, false, fpTables)
					} else if mode == "prechecked" {
						benchmarkVP9BareFPQuantizeEOB += e.quantizeVP9TxResidualFPWithQTablesPrechecked(
							dst, stride, tc.txSize, tc.txType, maxEob, scanOrder,
							dequant, out, qOut, false, false, fpTables)
					} else {
						benchmarkVP9BareFPQuantizeEOB += e.quantizeVP9TxResidualWithQTrellisFPTables(
							dst, stride, tc.txSize, tc.txType, dequant, 0, out, qOut,
							false, true, false, fpTables, nil)
					}
				}
			})
		}
	}
}
