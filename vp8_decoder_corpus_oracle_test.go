package govpx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

type generatedLibvpxCorpusCase struct {
	name                          string
	width                         int
	height                        int
	frames                        int
	args                          []string
	checkProfile                  bool
	wantProfile                   int
	checkTokenPartition           bool
	wantTokenPartition            vp8common.TokenPartition
	checkSegmentationMap          bool
	checkAllTokenPartitionsActive bool
}

func assertGeneratedLibvpxCorpusFeatures(t *testing.T, ivf []byte, tc generatedLibvpxCorpusCase) {
	t.Helper()
	if !tc.checkProfile && !tc.checkTokenPartition && !tc.checkSegmentationMap && !tc.checkAllTokenPartitionsActive {
		return
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset returned error: %v", err)
	}
	previousQuant := vp8dec.QuantHeader{}
	sawProfile := !tc.checkProfile
	sawTokenPartition := !tc.checkTokenPartition
	sawSegmentationMap := !tc.checkSegmentationMap
	sawAllTokenPartitionsActive := !tc.checkAllTokenPartitionsActive
	decoder, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	for frameIndex := 0; offset < len(ivf); frameIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, frameIndex)
		if err != nil {
			t.Fatalf("NextIVFFrame returned error: %v", err)
		}
		info, err := PeekVP8StreamInfo(frame.Data)
		if err != nil {
			t.Fatalf("PeekVP8StreamInfo returned error: %v", err)
		}
		if tc.checkProfile && info.Profile == tc.wantProfile {
			sawProfile = true
		}
		_, state, err := vp8dec.ParseStateHeader(frame.Data, previousQuant)
		if err != nil {
			t.Fatalf("ParseStateHeader returned error: %v", err)
		}
		if tc.checkTokenPartition && state.TokenPartition == tc.wantTokenPartition {
			sawTokenPartition = true
		}
		if tc.checkAllTokenPartitionsActive {
			header, err := vp8dec.ParseFrameHeader(frame.Data)
			if err != nil {
				t.Fatalf("ParseFrameHeader returned error: %v", err)
			}
			var layout vp8dec.PartitionLayout
			if err := vp8dec.ParsePartitionLayout(frame.Data, header, state.TokenPartition, &layout); err != nil {
				t.Fatalf("ParsePartitionLayout returned error: %v", err)
			}
			allActive := layout.TokenCount == int(1<<uint(tc.wantTokenPartition))
			for i := 0; i < layout.TokenCount; i++ {
				if len(layout.Tokens[i]) <= 1 {
					allActive = false
					break
				}
			}
			if allActive {
				sawAllTokenPartitionsActive = true
			}
		}
		if tc.checkSegmentationMap {
			if err := decoder.Decode(frame.Data); err != nil {
				t.Fatalf("Decode frame %d returned error while checking generated features: %v", frameIndex, err)
			}
			for _, segmentID := range decoder.segmentMap {
				if segmentID != 0 {
					sawSegmentationMap = true
					break
				}
			}
		}
		previousQuant = state.Quant
		offset = next
	}
	if !sawProfile {
		t.Fatalf("generated corpus profile = no frame with profile %d", tc.wantProfile)
	}
	if !sawTokenPartition {
		t.Fatalf("generated corpus token partition = no frame with partition %d", tc.wantTokenPartition)
	}
	if !sawSegmentationMap {
		t.Fatalf("generated corpus did not contain a nonzero segmentation map")
	}
	if !sawAllTokenPartitionsActive {
		t.Fatalf("generated corpus did not exercise all token partitions with active payload")
	}
}

func generateLibvpxCorpusIVF(t *testing.T, vpxenc string, dir string, tc generatedLibvpxCorpusCase) string {
	t.Helper()
	ivfPath := filepath.Join(dir, tc.name+".ivf")
	extraArgs := append([]string{"--end-usage=vbr"}, tc.args...)
	ivf, diag, err := coracle.VpxencVP8EncodeI420(
		deterministicI420Bytes(t, tc.width, tc.height, tc.frames),
		coracle.VpxencVP8Config{
			BinaryPath:        vpxenc,
			Width:             tc.width,
			Height:            tc.height,
			Frames:            tc.frames,
			Deadline:          "good",
			CPUUsed:           0,
			LagInFrames:       0,
			TargetBitrateKbps: 200,
			FPS:               "30/1",
			KeyFrameDistSet:   true,
			KeyFrameMinDist:   999,
			KeyFrameMaxDist:   999,
			OmitQuantizerArgs: true,
			ExtraArgs:         extraArgs,
		},
	)
	if err != nil {
		t.Fatalf("vpxenc failed: %v\n%s", err, diag)
	}
	if err := os.WriteFile(ivfPath, ivf, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return ivfPath
}

func deterministicI420Bytes(t *testing.T, width int, height int, frames int) []byte {
	t.Helper()
	if min(min(width, height), frames) <= 0 || width%2 != 0 || height%2 != 0 {
		t.Fatalf("invalid I420 corpus dimensions %dx%d frames=%d", width, height, frames)
	}
	uvWidth := width / 2
	uvHeight := height / 2
	buf := make([]byte, 0, frames*(width*height+2*uvWidth*uvHeight))
	for frame := range frames {
		for y := range height {
			for x := range width {
				buf = append(buf, byte((x*5+y*3+frame*17)&0xff))
			}
		}
		for y := range uvHeight {
			for x := range uvWidth {
				buf = append(buf, byte((96+x*3+y+frame*7)&0xff))
			}
		}
		for y := range uvHeight {
			for x := range uvWidth {
				buf = append(buf, byte((160+x+y*5+frame*11)&0xff))
			}
		}
	}
	return buf
}
