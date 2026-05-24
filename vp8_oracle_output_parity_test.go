//go:build govpx_oracle_trace

package govpx

import (
	"math"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

type oracleOutputParityCase struct {
	name          string
	validation    encoderValidationCase
	maxSizeGapPct float64
}

type oracleOutputPacket struct {
	key       bool
	show      bool
	qIndex    int
	sizeBytes int
}

// TestVP8OracleOutputParityMatrix is the CI-visible output-parity gate. It checks
// the emitted stream against libvpx by comparing libvpx-decoded frames and the
// packet-level encoder decisions that must stay byte-stable enough for WebRTC.
func TestVP8OracleOutputParityMatrix(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder output parity validation")
	}
	oracle := vp8test.NewChecksumOracle(t)
	vpxenc := vp8test.Vpxenc(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 4
	)

	cases := []oracleOutputParityCase{
		{
			name: "realtime-cbr-cpu0",
			validation: oracleOutputPanningCase(width, height, fps, targetKbps, frames, func(opts *EncoderOptions) {
				opts.Deadline = DeadlineRealtime
				opts.CpuUsed = 0
			}),
			maxSizeGapPct: 1.0,
		},
		{
			name: "realtime-cbr-cpu4",
			validation: oracleOutputPanningCase(width, height, fps, targetKbps, frames, func(opts *EncoderOptions) {
				opts.Deadline = DeadlineRealtime
				opts.CpuUsed = 4
			}),
			maxSizeGapPct: 1.0,
		},
		{
			name: "realtime-cbr-cpu8",
			validation: oracleOutputPanningCase(width, height, fps, targetKbps, frames, func(opts *EncoderOptions) {
				opts.Deadline = DeadlineRealtime
				opts.CpuUsed = 8
			}),
			maxSizeGapPct: 1.0,
		},
		{
			name: "good-quality-cbr-cpu5",
			validation: oracleOutputPanningCase(width, height, fps, targetKbps, frames, func(opts *EncoderOptions) {
				opts.Deadline = DeadlineGoodQuality
				opts.CpuUsed = 5
			}),
			maxSizeGapPct: 1.0,
		},
		{
			name: "realtime-cbr-error-resilient",
			validation: oracleOutputPanningCase(width, height, fps, targetKbps, frames, func(opts *EncoderOptions) {
				opts.Deadline = DeadlineRealtime
				opts.CpuUsed = 8
				opts.ErrorResilient = true
			}, "--error-resilient=1"),
			maxSizeGapPct: 5.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validation := tc.validation
			validation.name = "output-parity-" + tc.name
			sources := encoderValidationFrames(validation)
			got := encodeGopvxValidationCorpus(t, validation, sources)
			want := encodeLibvpxValidationCorpus(t, vpxenc, validation, sources)

			gotChecksums := oracle.Frames(t, got.ivf)
			wantChecksums := oracle.Frames(t, want)
			assertFrameChecksumsEqual(t, "encoded output decoded by libvpx", gotChecksums, wantChecksums)

			gotPackets := oracleOutputPackets(t, got.ivf)
			wantPackets := oracleOutputPackets(t, want)
			assertOracleOutputPacketsEqual(t, gotPackets, wantPackets, tc.maxSizeGapPct)
		})
	}
}

func oracleOutputPanningCase(width int, height int, fps int, targetKbps int, frames int, mutate func(*EncoderOptions), libvpxArgs ...string) encoderValidationCase {
	opts := encoderValidationOptions(width, height, fps, targetKbps, func(opts *EncoderOptions) {
		opts.KeyFrameInterval = 999
		if mutate != nil {
			mutate(opts)
		}
	})
	return encoderValidationCase{
		width:      width,
		height:     height,
		frames:     frames,
		fps:        fps,
		targetKbps: targetKbps,
		pattern:    encoderValidationPanning,
		opts:       opts,
		libvpxArgs: libvpxArgs,
	}
}

func oracleOutputPackets(t *testing.T, ivf []byte) []oracleOutputPacket {
	t.Helper()
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	var packets []oracleOutputPacket
	previousQuant := vp8dec.QuantHeader{}
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame[%d] returned error: %v", inputIndex, err)
		}
		_, state, err := vp8dec.ParseStateHeader(frame.Data, previousQuant)
		if err != nil {
			t.Fatalf("ParseStateHeader frame %d returned error: %v", inputIndex, err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo frame %d returned error: %v", inputIndex, err)
		}
		packets = append(packets, oracleOutputPacket{
			key:       info.KeyFrame,
			show:      info.ShowFrame,
			qIndex:    int(state.Quant.BaseQIndex),
			sizeBytes: len(frame.Data),
		})
		previousQuant = state.Quant
		offset = next
	}
	return packets
}

func assertOracleOutputPacketsEqual(t *testing.T, got []oracleOutputPacket, want []oracleOutputPacket, maxSizeGapPct float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("packet count = %d, want %d from libvpx", len(got), len(want))
	}
	for i := range want {
		if got[i].key != want[i].key {
			t.Fatalf("packet %d key = %t, want libvpx %t", i, got[i].key, want[i].key)
		}
		if got[i].show != want[i].show {
			t.Fatalf("packet %d show = %t, want libvpx %t", i, got[i].show, want[i].show)
		}
		if got[i].qIndex != want[i].qIndex {
			t.Fatalf("packet %d q_index = %d, want libvpx %d", i, got[i].qIndex, want[i].qIndex)
		}
		if want[i].sizeBytes <= 0 {
			t.Fatalf("packet %d libvpx size = %d, want >0", i, want[i].sizeBytes)
		}
		gapPct := math.Abs(float64(got[i].sizeBytes-want[i].sizeBytes)) * 100 / float64(want[i].sizeBytes)
		if gapPct > maxSizeGapPct {
			t.Fatalf("packet %d size = %d, libvpx = %d, gap %.2f%% exceeds %.2f%%",
				i, got[i].sizeBytes, want[i].sizeBytes, gapPct, maxSizeGapPct)
		}
	}
}
