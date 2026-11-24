package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestVP8EncoderSetAutoAltRefRejectsClosedEncoder(t *testing.T) {
	var nilEnc *govpx.VP8Encoder
	if err := nilEnc.SetAutoAltRef(true); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil encoder SetAutoAltRef error = %v, want ErrClosed", err)
	}

	e := newVP8FacadeEncoder(t)
	e.Close()
	if err := e.SetAutoAltRef(true); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed encoder SetAutoAltRef error = %v, want ErrClosed", err)
	}
}

func TestVP8EncoderSetScalingModeRejectsClosedEncoder(t *testing.T) {
	var nilEnc *govpx.VP8Encoder
	if err := nilEnc.SetScalingMode(govpx.ScalingNormal, govpx.ScalingNormal); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil encoder SetScalingMode error = %v, want ErrClosed", err)
	}

	e := newVP8FacadeEncoder(t)
	e.Close()
	if err := e.SetScalingMode(govpx.ScalingOneTwo, govpx.ScalingOneTwo); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed encoder SetScalingMode error = %v, want ErrClosed", err)
	}
}

func TestVP8EncoderSetFrameFlagsRejectsClosedEncoder(t *testing.T) {
	var nilEnc *govpx.VP8Encoder
	if err := nilEnc.SetFrameFlags(govpx.EncodeNoUpdateLast); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil encoder SetFrameFlags error = %v, want ErrClosed", err)
	}

	e := newVP8FacadeEncoder(t)
	e.Close()
	if err := e.SetFrameFlags(govpx.EncodeNoUpdateLast); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed encoder SetFrameFlags error = %v, want ErrClosed", err)
	}
}

func TestVP8EncoderSetScalingModeKeyframeCarriesScaleBits(t *testing.T) {
	cases := []struct {
		horiz     govpx.ScalingMode
		vert      govpx.ScalingMode
		wantHoriz int
		wantVert  int
	}{
		{govpx.ScalingNormal, govpx.ScalingNormal, 0, 0},
		{govpx.ScalingFourFive, govpx.ScalingFourFive, 1, 1},
		{govpx.ScalingThreeFive, govpx.ScalingThreeFive, 2, 2},
		{govpx.ScalingOneTwo, govpx.ScalingOneTwo, 3, 3},
		{govpx.ScalingFourFive, govpx.ScalingOneTwo, 1, 3},
		{govpx.ScalingNormal, govpx.ScalingThreeFive, 0, 2},
	}
	for _, tc := range cases {
		e := newVP8FacadeEncoder(t)
		if err := e.SetScalingMode(tc.horiz, tc.vert); err != nil {
			t.Fatalf("SetScalingMode(%v, %v) error = %v", tc.horiz, tc.vert, err)
		}
		src := newVP8FacadeImage(16, 16)
		dst := make([]byte, 8192)
		result, err := e.EncodeInto(dst, src, 0, 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto(%v, %v) error = %v", tc.horiz, tc.vert, err)
		}
		if !result.KeyFrame {
			t.Fatalf("after SetScalingMode(%v, %v) first frame not a key frame", tc.horiz, tc.vert)
		}
		header, err := vp8dec.ParseFrameHeader(result.Data)
		if err != nil {
			t.Fatalf("ParseFrameHeader error = %v", err)
		}
		if header.HorizScale != tc.wantHoriz {
			t.Fatalf("horiz scale bits = %d, want %d", header.HorizScale, tc.wantHoriz)
		}
		if header.VertScale != tc.wantVert {
			t.Fatalf("vert scale bits = %d, want %d", header.VertScale, tc.wantVert)
		}
		e.Close()
	}
}

func TestVP8EncoderResultAndLastQuantizerReportInternalQIndex(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	if _, _, ok := e.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer before encode returned ok")
	}
	if err := e.SetRateControl(govpx.RateControlConfig{
		Mode:                govpx.RateControlCQ,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		CQLevel:             32,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	}); err != nil {
		t.Fatalf("SetRateControl returned error: %v", err)
	}
	dst := make([]byte, 4096)
	result, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	wantInternal := vp8PacketBaseQIndex(t, result.Data)
	wantPublic := vp8common.QIndexToPublicQuantizer(wantInternal)
	if result.InternalQuantizer != wantInternal || result.Quantizer != wantPublic {
		t.Fatalf("EncodeResult quantizer = public:%d internal:%d, want public %d / internal %d",
			result.Quantizer, result.InternalQuantizer, wantPublic, wantInternal)
	}
	public, internal, ok := e.LastQuantizer()
	if !ok {
		t.Fatalf("LastQuantizer after encode returned !ok")
	}
	if public != result.Quantizer || internal != result.InternalQuantizer {
		t.Fatalf("LastQuantizer = public:%d internal:%d, want result public:%d internal:%d",
			public, internal, result.Quantizer, result.InternalQuantizer)
	}

	e.Reset()
	if _, _, ok := e.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer after Reset returned ok")
	}
}

func TestVP8EncoderTemporalScalabilityRejectsInvalidConfig(t *testing.T) {
	_, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		TemporalScalability: govpx.TemporalScalabilityConfig{Enabled: true, Mode: govpx.TemporalLayeringMode(13)},
	})
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidConfig", err)
	}

	_, err = govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		TemporalScalability: govpx.TemporalScalabilityConfig{Enabled: true, Mode: govpx.TemporalLayeringFiveLayers},
	})
	if !errors.Is(err, govpx.ErrInvalidBitrate) {
		t.Fatalf("five-layer default bitrate error = %v, want ErrInvalidBitrate", err)
	}

	_, err = govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   govpx.RateControlCBR,
		TargetBitrateKbps: 1200,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   govpx.TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [govpx.MaxTemporalLayers]int{900, 800},
		},
	})
	if !errors.Is(err, govpx.ErrInvalidBitrate) {
		t.Fatalf("non-monotonic bitrate error = %v, want ErrInvalidBitrate", err)
	}
}

func newVP8FacadeEncoder(t testing.TB) *govpx.VP8Encoder {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func vp8PacketBaseQIndex(t testing.TB, packet []byte) int {
	t.Helper()
	return int(vp8PacketStateHeader(t, packet).Quant.BaseQIndex)
}

func vp8PacketStateHeader(t testing.TB, packet []byte) vp8dec.StateHeader {
	t.Helper()
	coefProbs := vp8tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(
		packet,
		vp8dec.QuantHeader{},
		vp8dec.LoopFilterHeader{},
		&coefProbs,
		&modeProbs,
	)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	return state
}
