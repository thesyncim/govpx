//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9FullRDSub8x8Frame1Parity pins the GENUINE sub-8x8 joint RD producer
// (rdPickBestSub8x8Mode + encodeInterMbSegment + setAndCostBmiMvs + the
// per-sub-block NEWMV motion search) against the libvpx ground truth for the
// documented frame-1 SB0 16x16(0,0) child at mi=(0,1) BLOCK_4X4 ref=LAST
// EIGHTTAP (rdmult=139158 rddiv=7).
//
// libvpx ground truth (vpxenc-vp9 cpu0 CBR 1200 kbps, kf=999, fps 30, the
// panning source; TEMPORARY fprintf in rd_pick_best_sub8x8_mode +
// set_and_cost_bmi_mvs, reverted). Per-label decomposition (LABEL probe):
//
//	block 0: NEARESTMV mv=(9,15) brate=3989 byrate=3229 bdist=15809 bsse=22289 brdcost=3107734 eob=1
//	block 1: NEARESTMV mv=(9,15) brate=5226 byrate=4466 bdist=3769  bsse=29329 brdcost=1902822 eob=1
//	block 2: NEWMV     mv=(9,4)  brate=11906 byrate=7296 bdist=16990 bsse=24526 brdcost=5410688 eob=9
//	block 3: NEARESTMV mv=(9,4)  brate=33832 byrate=33072 bdist=23453 bsse=734187 brdcost=12197284 eob=16
//
// accumulating bsi->r=54953 bsi->d=60021 bsi->sse=810331 segment_rd=22618528.
//
// The NEWMV motion search (NEWMV probe) lands new_mv=(9,4) for block 2 from
// mvp_full=(1,1), ref_mv=(9,15).
func TestVP9FullRDSub8x8Frame1Parity(t *testing.T) {
	const width, height = 64, 64
	opts := VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlModeSet:  true,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		MaxKeyframeInterval: 999,
		Deadline:            DeadlineRealtime,
		CpuUsed:             0,
	}
	e, err := NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()

	var trace bytes.Buffer
	e.SetOracleTraceWriter(&trace)

	sources := vp9test.NewPanningSources(width, height, 256)
	dst := make([]byte, 1<<20)
	for i := 0; i < 2; i++ {
		if _, err := e.EncodeIntoWithResult(sources[i], dst); err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", i, err)
		}
	}

	cap, ok := e.vp9CapturedFullRDSub8x8()
	if !ok {
		t.Fatal("no sub-8x8 producer capture; the frame-1 SB0 64x64 NEWMV " +
			"EIGHTTAP_SMOOTH candidate did not score")
	}

	type want struct {
		mode       common.PredictionMode
		mv         vp9dec.MV
		modeMvRate int
		byrate     int
		bdist      uint64
		bsse       uint64
		brdcost    uint64
		eob        int
	}
	wants := [4]want{
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}, 3989 - 3229, 3229, 15809, 22289, 3107734, 1},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 15}, 5226 - 4466, 4466, 3769, 29329, 1902822, 1},
		{common.NewMv, vp9dec.MV{Row: 9, Col: 4}, 11906 - 7296, 7296, 16990, 24526, 5410688, 9},
		{common.NearestMv, vp9dec.MV{Row: 9, Col: 4}, 33832 - 33072, 33072, 23453, 734187, 12197284, 16},
	}

	for b := 0; b < 4; b++ {
		l := cap.Labels[b]
		w := wants[b]
		if !l.Valid {
			t.Errorf("block %d: label not produced", b)
			continue
		}
		if l.ModeMvRate != w.modeMvRate {
			t.Errorf("block %d mode/MV rate = %d, want %d (set_and_cost_bmi_mvs)",
				b, l.ModeMvRate, w.modeMvRate)
		}
		if l.Byrate != w.byrate {
			t.Errorf("block %d byrate = %d, want %d (cost_coeffs)", b, l.Byrate,
				w.byrate)
		}
		if l.Bdist != w.bdist {
			t.Errorf("block %d bdist = %d, want %d", b, l.Bdist, w.bdist)
		}
		if l.Bsse != w.bsse {
			t.Errorf("block %d bsse = %d, want %d", b, l.Bsse, w.bsse)
		}
		if l.Eob != w.eob {
			t.Errorf("block %d eob = %d, want %d", b, l.Eob, w.eob)
		}
		if l.Brdcost != w.brdcost {
			t.Errorf("block %d brdcost = %d, want %d", b, l.Brdcost, w.brdcost)
		}
	}

	// The block-2 NEWMV motion search must reproduce libvpx new_mv=(9,4).
	if !cap.SearchBlock2Valid {
		t.Error("block-2 NEWMV motion search did not run")
	} else if cap.SearchBlock2Mv != (vp9dec.MV{Row: 9, Col: 4}) {
		t.Errorf("block-2 NEWMV search MV = %+v, want (9,4)", cap.SearchBlock2Mv)
	}

	// The full genuine-derivation segment (rdPickBestSub8x8Mode, fed the libvpx
	// candidate context): per-label mode SELECTION + accumulation must reproduce
	// the libvpx BSI totals bsi->r=54953 d=60021 sse=810331 segment_rd=22618528
	// and pick NEARESTMV/NEARESTMV/NEWMV/NEARESTMV with (9,15)/(9,15)/(9,4)/(9,4).
	if !cap.SegValid {
		t.Fatal("genuine-derivation segment did not produce a result")
	}
	seg := cap.Seg
	const (
		wantR   = 54953
		wantD   = 60021
		wantSSE = 810331
		wantSeg = 22618528
	)
	if seg.R != wantR {
		t.Errorf("segment bsi->r = %d, want %d", seg.R, wantR)
	}
	if seg.D != wantD {
		t.Errorf("segment bsi->d = %d, want %d", seg.D, wantD)
	}
	if seg.SSE != wantSSE {
		t.Errorf("segment bsi->sse = %d, want %d", seg.SSE, wantSSE)
	}
	if seg.SegmentRD != wantSeg {
		t.Errorf("segment_rd = %d, want %d", seg.SegmentRD, wantSeg)
	}
	wantModes := [4]common.PredictionMode{
		common.NearestMv, common.NearestMv, common.NewMv, common.NearestMv,
	}
	wantMvs := [4]vp9dec.MV{
		{Row: 9, Col: 15}, {Row: 9, Col: 15}, {Row: 9, Col: 4}, {Row: 9, Col: 4},
	}
	for b := 0; b < 4; b++ {
		if seg.Labels[b].Mode != wantModes[b] {
			t.Errorf("segment label %d mode = %d, want %d", b,
				seg.Labels[b].Mode, wantModes[b])
		}
		if seg.Labels[b].Mv != wantMvs[b] {
			t.Errorf("segment label %d mv = %+v, want %+v", b,
				seg.Labels[b].Mv, wantMvs[b])
		}
	}
	if seg.Skippable {
		t.Error("segment skippable = true, want false (eobs nonzero)")
	}

	// VERT(4x8) partition-shape coverage: the (1,1) child BLOCK_4X8 segment.
	// libvpx BSI: r=73683 d=54955 sse=1751063 segment_rd=27060761, with
	// block 0 NEARESTMV(9,4) and block 1 NEWMV(16,-8) (the NEWMV search lands
	// (16,-8) from mvp=(9,4)). Exercises the multi-4x4-unit-per-label path
	// (each 4x8 label = two stacked 4x4 transform units).
	if !cap.VertValid {
		t.Fatal("VERT(4x8) segment did not produce a result")
	}
	v := cap.Vert
	if v.R != 73683 {
		t.Errorf("VERT bsi->r = %d, want 73683", v.R)
	}
	if v.D != 54955 {
		t.Errorf("VERT bsi->d = %d, want 54955", v.D)
	}
	if v.SSE != 1751063 {
		t.Errorf("VERT bsi->sse = %d, want 1751063", v.SSE)
	}
	if v.SegmentRD != 27060761 {
		t.Errorf("VERT segment_rd = %d, want 27060761", v.SegmentRD)
	}
	if v.Labels[0].Mode != common.NearestMv ||
		v.Labels[0].Mv != (vp9dec.MV{Row: 9, Col: 4}) {
		t.Errorf("VERT label 0 = %d/%+v, want NearestMv/(9,4)",
			v.Labels[0].Mode, v.Labels[0].Mv)
	}
	if v.Labels[1].Mode != common.NewMv ||
		v.Labels[1].Mv != (vp9dec.MV{Row: 16, Col: -8}) {
		t.Errorf("VERT label 1 = %d/%+v, want NewMv/(16,-8)",
			v.Labels[1].Mode, v.Labels[1].Mv)
	}
}
