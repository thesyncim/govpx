//go:build govpx_oracle_trace

package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9FullRDPredMvSearchExitFrame2Regression(t *testing.T) {
	vp9test.RequireVpxenc(t)

	prevP := vp9InterUseDeepRDPartition
	prevTh := vp9InterUseDeepRDThisRDScore
	prevS := vp9InterUseDeepRDSub8x8
	prevRB := vp9InterUseDeepRDRefBestRD
	vp9InterUseDeepRDPartition = true
	vp9InterUseDeepRDThisRDScore = true
	vp9InterUseDeepRDSub8x8 = true
	vp9InterUseDeepRDRefBestRD = true
	t.Cleanup(func() {
		vp9InterUseDeepRDPartition = prevP
		vp9InterUseDeepRDThisRDScore = prevTh
		vp9InterUseDeepRDSub8x8 = prevS
		vp9InterUseDeepRDRefBestRD = prevRB
	})

	const width, height, frames = 64, 64, 4
	sources := newVP9NextDivPanningSources(width, height, frames)
	govpxFrames := encodeVP9FullRDPredMvRegressionFrames(t, sources)
	libvpxFrames := vp9test.VpxencPackets(t, sources,
		"--end-usage=cbr",
		"--target-bitrate=1200",
		"--cpu-used=0",
		"--kf-min-dist=0",
		"--kf-max-dist=999",
		"--buf-sz=600",
		"--buf-initial-sz=400",
		"--buf-optimal-sz=500",
		"--drop-frame=0",
		"--timebase=1/30",
	)
	if len(govpxFrames) < 3 || len(libvpxFrames) < 3 {
		t.Fatalf("frame count govpx=%d libvpx=%d, want at least 3/3",
			len(govpxFrames), len(libvpxFrames))
	}

	govpxDec := decodeVP9FramesForMiGrid(t, govpxFrames[:3])
	defer govpxDec.Close()
	libvpxDec := decodeVP9FramesForMiGrid(t, libvpxFrames[:3])
	defer libvpxDec.Close()

	const miCols = 8
	got := govpxDec.miGrid[1*miCols+2]
	want := libvpxDec.miGrid[1*miCols+2]
	if want.SbType != common.Block8x8 ||
		want.RefFrame[0] != vp9dec.LastFrame ||
		want.Mode != common.NewMv ||
		want.Mv[0] != (vp9dec.MV{Row: 27, Col: -7}) ||
		want.TxSize != common.Tx8x8 ||
		want.Skip != 0 {
		t.Fatalf("libvpx frame2 mi(1,2) anchor drifted: %s",
			nextDivFmtCommitted(&want))
	}
	if got != want {
		t.Fatalf("frame2 mi(1,2) pred-mv replay regression:\n  got  %s\n  want %s",
			nextDivFmtCommitted(&got), nextDivFmtCommitted(&want))
	}
}

func encodeVP9FullRDPredMvRegressionFrames(t testing.TB,
	sources []*image.YCbCr,
) [][]byte {
	t.Helper()
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

	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		pkt, encErr := e.Encode(src)
		if encErr != nil {
			t.Fatalf("Encode frame %d: %v", i, encErr)
		}
		out = append(out, append([]byte(nil), pkt...))
	}
	return out
}

func decodeVP9FramesForMiGrid(t testing.TB, frames [][]byte) *VP9Decoder {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for i, pkt := range frames {
		if err := d.Decode(pkt); err != nil {
			d.Close()
			t.Fatalf("Decode frame %d: %v", i, err)
		}
		if i < len(frames)-1 {
			if _, ok := d.NextFrame(); !ok {
				d.Close()
				t.Fatalf("NextFrame after frame %d", i)
			}
		}
	}
	return d
}
