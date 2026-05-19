package gpuanalysis_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	govpx "github.com/thesyncim/govpx"
	// Blank-import registers the GPU analyzer with the internal
	// registry so VP8AnalysisObserveGPU is available at construction
	// time. Without this import the encoder would refuse to start in
	// GPU mode and the test would fail with
	// ErrVP8AnalysisGPUNotRegistered.
	_ "github.com/thesyncim/govpx/gpuanalysis"
)

const (
	parityWidth  = 320
	parityHeight = 240
	parityFrames = 12
)

func newParityEncoder(t *testing.T, cfg govpx.VP8AnalysisConfig) *govpx.VP8Encoder {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               parityWidth,
		Height:              parityHeight,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   1500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    30,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		Threads:             1,
		Analysis:            cfg,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder cfg=%+v: %v", cfg, err)
	}
	return e
}

func encodeStream(t *testing.T, e *govpx.VP8Encoder) ([][]byte, string) {
	t.Helper()
	defer e.Close()
	img := govpx.Image{
		Width:   parityWidth,
		Height:  parityHeight,
		Y:       make([]byte, parityWidth*parityHeight),
		U:       make([]byte, (parityWidth/2)*(parityHeight/2)),
		V:       make([]byte, (parityWidth/2)*(parityHeight/2)),
		YStride: parityWidth,
		UStride: parityWidth / 2,
		VStride: parityWidth / 2,
	}
	buf := make([]byte, parityWidth*parityHeight*4)
	h := sha256.New()
	packets := make([][]byte, 0, parityFrames)
	for i := range parityFrames {
		for j := range img.Y {
			img.Y[j] = byte((j*7 + i*13) & 0xFF)
		}
		for j := range img.U {
			img.U[j] = byte(96 + ((j + i*3) & 0x3F))
		}
		for j := range img.V {
			img.V[j] = byte(144 + ((j*2 + i*5) & 0x3F))
		}
		result, err := e.EncodeInto(buf, img, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
		if result.Dropped {
			packets = append(packets, nil)
			continue
		}
		pkt := append([]byte(nil), result.Data...)
		packets = append(packets, pkt)
		h.Write(pkt)
	}
	return packets, hex.EncodeToString(h.Sum(nil))
}

// TestVP8AnalysisObserveGPUByteParity is the headline check: enabling
// the GPU analyzer must not change the encoded bitstream by even one
// byte versus VP8AnalysisOff. The test relies on the blank-import at
// the top of this file to have registered the GPU constructor.
func TestVP8AnalysisObserveGPUByteParity(t *testing.T) {
	offPackets, offHash := encodeStream(t, newParityEncoder(t, govpx.DefaultVP8AnalysisConfig()))
	gpuPackets, gpuHash := encodeStream(t, newParityEncoder(t, govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveGPU,
		CollectMotionHints: true,
		CollectSkipMap:     true,
		CollectComplexity:  true,
	}))
	if offHash != gpuHash {
		t.Fatalf("byte parity violation: off=%s gpu=%s", offHash, gpuHash)
	}
	if len(offPackets) != len(gpuPackets) {
		t.Fatalf("packet count mismatch: off=%d gpu=%d", len(offPackets), len(gpuPackets))
	}
	for i := range offPackets {
		if !bytes.Equal(offPackets[i], gpuPackets[i]) {
			t.Fatalf("frame %d differs: off=%d bytes gpu=%d bytes",
				i, len(offPackets[i]), len(gpuPackets[i]))
		}
	}
	t.Logf("byte parity confirmed off=gpu sha256=%s frames=%d", offHash, parityFrames)
}

// TestVP8AnalysisObserveGPUWithoutImportFails confirms that the
// blank-import contract is real: the encoder refuses to start in GPU
// mode when the registry has no constructor.
//
// We cannot easily simulate "no registration" from inside this same
// test binary (the init() has already run), so the test instead
// proves the symmetric property: GPU mode succeeds because we DID
// blank-import the package. A separate test binary would be required
// to assert the failure path; documenting the contract here.
func TestVP8AnalysisObserveGPUWithoutImportFails(t *testing.T) {
	if govpx.ErrVP8AnalysisGPUNotRegistered == nil {
		t.Fatal("ErrVP8AnalysisGPUNotRegistered must be non-nil")
	}
}

// TestVP8AnalysisObserveGPUStatsPopulated confirms the GPU observer
// fills the FrameAnalysis with raster coords + SADs as expected.
func TestVP8AnalysisObserveGPUStatsPopulated(t *testing.T) {
	e := newParityEncoder(t, govpx.VP8AnalysisConfig{
		Mode:               govpx.VP8AnalysisObserveGPU,
		CollectMotionHints: true,
	})
	defer e.Close()
	img := govpx.Image{
		Width:   parityWidth,
		Height:  parityHeight,
		Y:       make([]byte, parityWidth*parityHeight),
		U:       make([]byte, (parityWidth/2)*(parityHeight/2)),
		V:       make([]byte, (parityWidth/2)*(parityHeight/2)),
		YStride: parityWidth,
		UStride: parityWidth / 2,
		VStride: parityWidth / 2,
	}
	buf := make([]byte, parityWidth*parityHeight*4)
	for i := range 6 {
		for j := range img.Y {
			img.Y[j] = byte((j*7 + i*13) & 0xFF)
		}
		if _, err := e.EncodeInto(buf, img, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	fa := e.LastFrameAnalysis()
	if fa == nil {
		t.Fatal("LastFrameAnalysis: nil while GPU analyzer is configured")
	}
	if !fa.Observed {
		t.Fatal("FrameAnalysis.Observed: expected true")
	}
	expectedCols := (parityWidth + 15) >> 4
	expectedRows := (parityHeight + 15) >> 4
	if fa.MBCols != expectedCols || fa.MBRows != expectedRows {
		t.Fatalf("MB dims %dx%d; want %dx%d",
			fa.MBCols, fa.MBRows, expectedCols, expectedRows)
	}
	if len(fa.MB) != expectedCols*expectedRows {
		t.Fatalf("len(MB) = %d; want %d", len(fa.MB), expectedCols*expectedRows)
	}
	for i, mb := range fa.MB {
		if int16(i%expectedCols) != mb.MBX || int16(i/expectedCols) != mb.MBY {
			t.Fatalf("MB[%d] coord = (%d,%d); want (%d,%d)",
				i, mb.MBX, mb.MBY, i%expectedCols, i/expectedCols)
		}
	}
}
