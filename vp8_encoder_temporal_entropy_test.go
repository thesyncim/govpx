package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestEncodeIntoRefreshesEntropyUnlessDisabled(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	first := testImage(16, 16)
	second := rateControlTestFrame(16, 16, 1)
	fillImage(first, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if !packetState(t, key.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("key refresh entropy = false, want libvpx default true")
	}
	keyData := append([]byte(nil), key.Data...)
	inter, err := e.EncodeInto(dst, second, 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if inter.KeyFrame {
		t.Fatalf("inter KeyFrame = true, want interframe")
	}
	if !packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("inter refresh entropy = false, want libvpx default true")
	}
	interData := append([]byte(nil), inter.Data...)
	decoded := decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("decoded frame count = %d, want 2", len(decoded))
	}

	e = newEntropyRefreshTestEncoder(t, false)
	key, err = e.EncodeInto(dst, first, 0, 1, 0)
	if err != nil {
		t.Fatalf("second key EncodeInto returned error: %v", err)
	}
	keyData = append([]byte(nil), key.Data...)
	inter, err = e.EncodeInto(dst, second, 1, 1, EncodeNoUpdateEntropy)
	if err != nil {
		t.Fatalf("no-update-entropy inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("no-update-entropy inter refresh entropy = true, want false")
	}
	interData = append([]byte(nil), inter.Data...)
	decoded = decodeFrameSequence(t, keyData, interData)
	if len(decoded) != 2 {
		t.Fatalf("no-update-entropy decoded frame count = %d, want 2", len(decoded))
	}
}

func TestEncodeIntoForcedKeyHonorsNoUpdateEntropy(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	dst := make([]byte, 8192)

	for i := range 3 {
		src := rateControlTestFrame(16, 16, i)
		if _, err := e.EncodeInto(dst, src, uint64(i), 1, 0); err != nil {
			t.Fatalf("warm frame %d EncodeInto returned error: %v", i, err)
		}
	}
	forced, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 3), 3, 1, EncodeForceKeyFrame|EncodeNoUpdateEntropy)
	if err != nil {
		t.Fatalf("forced key EncodeInto returned error: %v", err)
	}
	if !forced.KeyFrame {
		t.Fatalf("forced KeyFrame = false, want true")
	}
	if packetState(t, forced.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("forced key refresh entropy = true, want libvpx no-update flag honored")
	}
}

func TestEncodeNoUpdateEntropyCarriesAcrossDroppedFrame(t *testing.T) {
	e, err := NewVP8Encoder(EncoderOptions{
		Width:              64,
		Height:             64,
		FPS:                30,
		RateControlMode:    RateControlCBR,
		TargetBitrateKbps:  300,
		MinQuantizer:       4,
		MaxQuantizer:       56,
		Deadline:           DeadlineRealtime,
		CpuUsed:            -3,
		KeyFrameInterval:   999,
		Tuning:             TunePSNR,
		DropFrameAllowed:   true,
		DropFrameWaterMark: defaultDropFramesWaterMark,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	dst := make([]byte, 1<<20)
	update := RealtimeTarget{
		BitrateKbps:  700,
		FPS:          15,
		MinQuantizer: 2,
		MaxQuantizer: 48,
		FrameDrop:    RealtimeFrameDropEnabled,
	}
	encode := func(frame int, flags EncodeFlags) EncodeResult {
		t.Helper()
		result, err := e.EncodeInto(dst, encoderValidationPanningFrame(64, 64, frame), uint64(frame), 1, flags)
		if err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", frame, err)
		}
		return result
	}

	encode(0, 0)
	if err := e.SetRealtimeTarget(update); err != nil {
		t.Fatalf("SetRealtimeTarget returned error: %v", err)
	}
	encodedNoUpdate := encode(1, EncodeNoUpdateEntropy)
	if encodedNoUpdate.Dropped {
		t.Fatalf("frame 1 dropped, want emitted setup frame")
	}
	if packetState(t, encodedNoUpdate.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("frame 1 refresh entropy = true, want no-update flag honored")
	}
	if e.carriedNoUpdateEntropy {
		t.Fatalf("carried no-update entropy after emitted frame = true, want cleared")
	}

	if !encode(2, 0).Dropped {
		t.Fatalf("frame 2 emitted, want decimation drop")
	}
	if err := e.SetRealtimeTarget(update); err != nil {
		t.Fatalf("second SetRealtimeTarget returned error: %v", err)
	}
	if err := e.SetARNR(0, 0, 1); err != nil {
		t.Fatalf("SetARNR returned error: %v", err)
	}
	encode(3, 0)
	if err := e.SetRealtimeTarget(update); err != nil {
		t.Fatalf("third SetRealtimeTarget returned error: %v", err)
	}
	if !encode(4, EncodeNoUpdateEntropy).Dropped {
		t.Fatalf("frame 4 emitted, want no-update entropy frame to drop")
	}
	if !e.carriedNoUpdateEntropy {
		t.Fatalf("carried no-update entropy after dropped frame = false, want true")
	}

	next := encode(5, 0)
	if next.Dropped {
		t.Fatalf("frame 5 dropped, want emitted carryover frame")
	}
	if packetState(t, next.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("frame 5 refresh entropy = true, want dropped no-update flag to carry over")
	}
	if e.carriedNoUpdateEntropy {
		t.Fatalf("carried no-update entropy after emitted carryover frame = true, want cleared")
	}
}

func TestEncodeIntoErrorResilientUsesTransientEntropyUpdates(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, true)
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if keyState.Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient key refresh entropy = true, want libvpx forced false")
	}
	if keyState.Probability.UpdateCount == 0 {
		t.Fatalf("error-resilient key coefficient updates = 0, want transient updates")
	}
	committedKeyProbs := e.coefProbs
	if committedKeyProbs != vp8tables.DefaultCoefProbs {
		t.Fatalf("error-resilient key committed coefficient probabilities, want default snapshot")
	}

	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient inter refresh entropy = true, want false")
	}
	if e.coefProbs != committedKeyProbs {
		t.Fatalf("error-resilient inter committed transient coefficient probabilities")
	}
}

func TestEncodeIntoErrorResilientPartitionsRefreshesKeyEntropyOnly(t *testing.T) {
	e := newEntropyRefreshTestEncoder(t, false)
	e.opts.ErrorResilientPartitions = true
	src := testImage(16, 16)
	fillImage(src, 180, 90, 170)
	dst := make([]byte, 8192)

	key, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	keyState := packetState(t, key.Data)
	if !keyState.Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient-partitions key refresh entropy = false, want libvpx forced true")
	}
	committedKeyProbs := e.coefProbs

	inter, err := e.EncodeInto(dst, rateControlTestFrame(16, 16, 2), 1, 1, 0)
	if err != nil {
		t.Fatalf("inter EncodeInto returned error: %v", err)
	}
	if packetState(t, inter.Data).Refresh.RefreshEntropyProbs {
		t.Fatalf("error-resilient-partitions inter refresh entropy = true, want false")
	}
	if e.coefProbs != committedKeyProbs {
		t.Fatalf("error-resilient-partitions inter committed transient coefficient probabilities")
	}
}

func TestCoefficientEntropySavingsUsesIndependentContextWhenErrorResilient(t *testing.T) {
	// The independent-context coefficient entropy-savings path mirrors
	// libvpx's VPX_ERROR_RESILIENT_PARTITIONS branch (bit 0x2). The plain
	// `--error-resilient=1` (DEFAULT, bit 0x1) does NOT enable that branch
	// in libvpx; only the partitions mode does. govpx exposes this as
	// EncoderOptions.ErrorResilientPartitions; the simpler ErrorResilient
	// bool stays on the default coef-savings path so the keyframe coef-prob
	// emission stays byte-equivalent with libvpx's `--error-resilient=1`.
	e := &VP8Encoder{
		opts: EncoderOptions{
			Width:                    16,
			Height:                   16,
			ErrorResilientPartitions: true,
		},
		coefProbs: vp8tables.DefaultCoefProbs,
		interFrameModes: []vp8enc.InterFrameMacroblockMode{{
			RefFrame: vp8common.LastFrame,
			Mode:     vp8common.ZeroMV,
		}},
		keyFrameCoeffs: make([]vp8enc.MacroblockCoefficients, 1),
		tokenAbove:     make([]vp8enc.TokenContextPlanes, 1),
	}
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					e.coefProbs[block][band][ctx][node] = 1
				}
			}
		}
	}
	e.keyFrameCoeffs[0].QCoeff[0][0] = 1
	e.keyFrameCoeffs[0].SetBlockEOB(0, 1)
	got := e.coefficientEntropySavingsBits(false, 1)
	above := make([]vp8enc.TokenContextPlanes, 1)
	want, err := vp8enc.InterCoefficientEntropySavingsIndependent(1, 1, e.interFrameModes, e.keyFrameCoeffs, above, &e.coefProbs)
	if err != nil {
		t.Fatalf("InterCoefficientEntropySavingsIndependent returned error: %v", err)
	}
	if got != want {
		t.Fatalf("error-resilient coefficient entropy savings = %d, want independent-context savings %d", got, want)
	}
	if got == 0 {
		t.Fatalf("error-resilient coefficient entropy savings = 0, want recode accounting to include independent-context branch")
	}
}
