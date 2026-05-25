package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

// TestVP9LoopFilterStrengthFromQAtSpeed8 covers libvpx's LPF_PICK_FROM_Q
// path (vp9_picklpf.c:168-198). At cpu_used=8 the speed-features
// dispatcher sets sf.lpf_pick = LPF_PICK_FROM_Q for speed >= 3 (libvpx
// vp9_speed_features.c:555). The encoded header's FilterLevel must
// equal the closed-form formula filt_guess = ROUND_POWER_OF_TWO(q *
// 20723 + 1015158, 18), with the KEY_FRAME -4 bias applied.
func TestVP9LoopFilterStrengthFromQAtSpeed8(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:    width,
		Height:   height,
		FPS:      30,
		CpuUsed:  8,
		Deadline: DeadlineRealtime,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if e.sf.LpfPick != LpfPickFromQ {
		t.Fatalf("CpuUsed=8 sf.LpfPick=%v, want LpfPickFromQ", e.sf.LpfPick)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(width, height, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	qindex := int(hdr.Quant.BaseQindex)
	// libvpx vp9_picklpf.c:177 — 8-bit: filt_guess = ROUND_POWER_OF_TWO(q * 20723 + 1015158, 18).
	q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
	want := (q*20723 + 1015158 + (1 << 17)) >> 18
	// libvpx vp9_picklpf.c:197 — KEY_FRAME bias.
	want -= 4
	if want < 0 {
		want = 0
	}
	if want > vp9dec.MaxLoopFilter {
		want = vp9dec.MaxLoopFilter
	}
	if int(hdr.Loopfilter.FilterLevel) != want {
		t.Fatalf("FilterLevel=%d, want %d (qindex=%d ac_q=%d)",
			hdr.Loopfilter.FilterLevel, want, qindex, q)
	}
}

// TestVP9LoopFilterFromQFormulaParity exercises the FROM_Q closed-form
// directly across the qindex sweep [0, 255] for both KEY_FRAME and
// non-KEY_FRAME, asserting the govpx port matches libvpx's exact
// arithmetic (vp9_picklpf.c:175-198, 8-bit branch).
func TestVP9LoopFilterFromQFormulaParity(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	for qindex := 0; qindex <= 255; qindex++ {
		for _, isKey := range []bool{false, true} {
			got := e.vp9PickLpfFromQ(qindex, isKey /*segEnabled=*/, false, 64, 64)
			q := int(vp9dec.VpxAcQuant(qindex, 0, vp9dec.BitDepth8))
			want := (q*20723 + 1015158 + (1 << 17)) >> 18
			if isKey {
				want -= 4
			}
			if want < 0 {
				want = 0
			}
			if want > vp9dec.MaxLoopFilter {
				want = vp9dec.MaxLoopFilter
			}
			if got != want {
				t.Fatalf("qindex=%d isKey=%v: got %d, want %d", qindex, isKey, got, want)
			}
		}
	}
}

// TestVP9LoopFilterMinimalLpfZerosWhenLastNonzero covers
// LPF_PICK_MINIMAL_LPF (vp9_picklpf.c:166-167). When the previous
// frame's filter level was non-zero, the picker zeros this frame's
// level; otherwise it leaves it at zero.
func TestVP9LoopFilterMinimalLpfZerosWhenLastNonzero(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  64,
		Height: 64,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	e.vp9LastFiltLevel = 17
	if got := e.vp9PickLpfMinimal(); got != 0 {
		t.Fatalf("vp9LastFiltLevel=17 vp9PickLpfMinimal=%d, want 0", got)
	}
	e.vp9LastFiltLevel = 0
	if got := e.vp9PickLpfMinimal(); got != 0 {
		t.Fatalf("vp9LastFiltLevel=0 vp9PickLpfMinimal=%d, want 0", got)
	}
}

// TestVP9PickLpfMaxFilterLevelOnePass asserts get_max_filter_level
// returns MAX_LOOP_FILTER for one-pass (vp9_picklpf.c:41-43).
func TestVP9PickLpfMaxFilterLevelOnePass(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if got := e.vp9PickLpfMaxFilterLevel(false); got != vp9dec.MaxLoopFilter {
		t.Fatalf("vp9PickLpfMaxFilterLevel(one-pass)=%d, want %d",
			got, vp9dec.MaxLoopFilter)
	}
}
