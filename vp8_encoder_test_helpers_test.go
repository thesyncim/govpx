package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func newTestEncoder(tb testing.TB) *VP8Encoder {
	tb.Helper()
	return newSizedTestEncoder(tb, 16, 16)
}

func newSizedTestEncoder(tb testing.TB, width int, height int) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  defaultDropFramesWaterMark,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newTemporalTestEncoder(tb testing.TB, temporal TemporalScalabilityConfig) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  defaultDropFramesWaterMark,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      true,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: temporal,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newTemporalRefreshFlagTestEncoder(tb testing.TB, temporal TemporalScalabilityConfig) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    true,
		DropFrameWaterMark:  defaultDropFramesWaterMark,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
		TemporalScalability: temporal,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newAdaptiveSceneCutTestEncoder(tb testing.TB, adaptive bool) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:             32,
		Height:            32,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 1200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  120,
		AdaptiveKeyFrames: adaptive,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newEntropyRefreshTestEncoder(tb testing.TB, errorResilient bool) *VP8Encoder {
	tb.Helper()
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		ErrorResilient:      errorResilient,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		tb.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func newLowBitrateDropTestEncoder(t *testing.T, dropFrameAllowed bool) *VP8Encoder {
	t.Helper()
	dropFrameWaterMark := 0
	if dropFrameAllowed {
		dropFrameWaterMark = defaultDropFramesWaterMark
	}
	e, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		DropFrameAllowed:    dropFrameAllowed,
		DropFrameWaterMark:  dropFrameWaterMark,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 0,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	return e
}

func rateControlTestFrame(width int, height int, index int) Image {
	img := testImage(width, height)
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}

func testImage(width int, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func fillImage(img Image, y byte, u byte, v byte) {
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
	}
	for i := range img.V {
		img.V[i] = v
	}
}

func fillMacroblock(img Image, mbRow int, mbCol int, y byte, u byte, v byte) {
	y0 := mbRow * 16
	x0 := mbCol * 16
	for row := y0; row < y0+16 && row < img.Height; row++ {
		for col := x0; col < x0+16 && col < img.Width; col++ {
			img.Y[row*img.YStride+col] = y
		}
	}
	uvHeight := (img.Height + 1) >> 1
	uvWidth := (img.Width + 1) >> 1
	uvY0 := mbRow * 8
	uvX0 := mbCol * 8
	for row := uvY0; row < uvY0+8 && row < uvHeight; row++ {
		for col := uvX0; col < uvX0+8 && col < uvWidth; col++ {
			img.U[row*img.UStride+col] = u
			img.V[row*img.VStride+col] = v
		}
	}
}

func packetTokenPartition(t *testing.T, packet []byte) vp8common.TokenPartition {
	t.Helper()
	return packetState(t, packet).TokenPartition
}

func packetBaseQIndex(t *testing.T, packet []byte) int {
	t.Helper()
	return int(packetState(t, packet).Quant.BaseQIndex)
}

func packetState(t *testing.T, packet []byte) vp8dec.StateHeader {
	t.Helper()
	var coefProbs = vp8tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	return state
}

func shiftImageRightOne(src Image) Image {
	dst := testImage(src.Width, src.Height)
	for row := 0; row < src.Height; row++ {
		dst.Y[row*dst.YStride] = src.Y[row*src.YStride]
		for col := 1; col < src.Width; col++ {
			dst.Y[row*dst.YStride+col] = src.Y[row*src.YStride+col-1]
		}
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	buffers.CopyPlane(dst.U, dst.UStride, src.U, src.UStride, uvWidth, uvHeight)
	buffers.CopyPlane(dst.V, dst.VStride, src.V, src.VStride, uvWidth, uvHeight)
	return dst
}

func copyShifted8x8FromImage(dst Image, src Image, y int, x int, dy int, dx int) {
	for row := range 8 {
		for col := range 8 {
			dst.Y[(y+row)*dst.YStride+x+col] = src.Y[(y+row+dy)*src.YStride+x+col+dx]
		}
	}
}

func decodeSingleFrame(tb testing.TB, packet []byte) Image {
	tb.Helper()
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		tb.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		tb.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		tb.Fatalf("NextFrame returned no frame")
	}
	return frame
}

func parseEncoderStateHeader(t *testing.T, packet []byte) vp8dec.StateHeader {
	t.Helper()
	var coefProbs = vp8tables.DefaultCoefProbs
	var modeProbs vp8dec.ModeProbs
	vp8dec.ResetModeProbs(&modeProbs)
	_, state, _, err := vp8dec.ParseStateHeaderWithReaderAndProbsAndLoopFilter(packet, vp8dec.QuantHeader{}, vp8dec.LoopFilterHeader{}, &coefProbs, &modeProbs)
	if err != nil {
		t.Fatalf("ParseStateHeaderWithReaderAndProbsAndLoopFilter returned error: %v", err)
	}
	return state
}

func assertImagesEqual(t *testing.T, name string, want Image, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertPlaneEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertPlaneEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight)
	assertPlaneEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight)
}

func assertPlaneEqual(t *testing.T, name string, want []byte, wantStride int, got []byte, gotStride int, width int, height int) {
	t.Helper()
	for row := range height {
		wantRow := want[row*wantStride : row*wantStride+width]
		gotRow := got[row*gotStride : row*gotStride+width]
		for col := range width {
			if gotRow[col] != wantRow[col] {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, row, col, gotRow[col], wantRow[col])
			}
		}
	}
}

func assertMacroblockEqual(t *testing.T, name string, want Image, got Image, mbRow int, mbCol int) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertPlaneBlockEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height, mbRow*16, mbCol*16, 16, 16)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertPlaneBlockEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
	assertPlaneBlockEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
}

func assertMacroblockDifferent(t *testing.T, name string, a Image, b Image, mbRow int, mbCol int) {
	t.Helper()
	if a.Width != b.Width || a.Height != b.Height {
		t.Fatalf("%s dimensions differ: %dx%d vs %dx%d", name, a.Width, a.Height, b.Width, b.Height)
	}
	if macroblockEqual(a, b, mbRow, mbCol) {
		t.Fatalf("%s macroblock (%d,%d) matches previous frame; want active MB to update", name, mbRow, mbCol)
	}
}

func assertPlaneBlockEqual(t *testing.T, name string, want []byte, wantStride int, got []byte, gotStride int, planeWidth int, planeHeight int, startRow int, startCol int, blockWidth int, blockHeight int) {
	t.Helper()
	width := min(blockWidth, planeWidth-startCol)
	height := min(blockHeight, planeHeight-startRow)
	for row := range height {
		for col := range width {
			wantValue := want[(startRow+row)*wantStride+startCol+col]
			gotValue := got[(startRow+row)*gotStride+startCol+col]
			if gotValue != wantValue {
				t.Fatalf("%s[%d,%d] = %d, want %d", name, startRow+row, startCol+col, gotValue, wantValue)
			}
		}
	}
}

func macroblockEqual(a Image, b Image, mbRow int, mbCol int) bool {
	if !planeBlockEqual(a.Y, a.YStride, b.Y, b.YStride, a.Width, a.Height, mbRow*16, mbCol*16, 16, 16) {
		return false
	}
	uvWidth := (a.Width + 1) >> 1
	uvHeight := (a.Height + 1) >> 1
	return planeBlockEqual(a.U, a.UStride, b.U, b.UStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8) &&
		planeBlockEqual(a.V, a.VStride, b.V, b.VStride, uvWidth, uvHeight, mbRow*8, mbCol*8, 8, 8)
}

func planeBlockEqual(a []byte, aStride int, b []byte, bStride int, planeWidth int, planeHeight int, startRow int, startCol int, blockWidth int, blockHeight int) bool {
	width := min(blockWidth, planeWidth-startCol)
	height := min(blockHeight, planeHeight-startRow)
	for row := range height {
		for col := range width {
			if a[(startRow+row)*aStride+startCol+col] != b[(startRow+row)*bStride+startCol+col] {
				return false
			}
		}
	}
	return true
}
