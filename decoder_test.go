package libgopx

import (
	"errors"
	"testing"

	vp8common "github.com/thesyncim/libgopx/internal/vp8/common"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	vp8tables "github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestNewVP8DecoderValidation(t *testing.T) {
	_, err := NewVP8Decoder(DecoderOptions{Threads: -1})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeRequiresInitialKeyFrame(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8InterFramePacket(0, 0, true))
	if !errors.Is(err, ErrNeedKeyFrame) {
		t.Fatalf("error = %v, want ErrNeedKeyFrame", err)
	}
}

func TestDecodeStubReturnsUnsupportedAfterValidation(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{MaxWidth: 640, MaxHeight: 480})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.DecodeWithPTS(vp8KeyFramePacketWithPayload(320, 240, 200, 0, true), 44)
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("error = %v, want ErrUnsupportedFeature", err)
	}
	if d.lastInfo.Width != 320 || d.lastInfo.Height != 240 || d.lastInfo.PTS != 44 {
		t.Fatalf("lastInfo = %+v, want validated frame metadata", d.lastInfo)
	}
}

func TestDecodeInitializesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(5, 3, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.current.Img.Width != 5 || d.current.Img.Height != 3 {
		t.Fatalf("current visible dimensions = %dx%d, want 5x3", d.current.Img.Width, d.current.Img.Height)
	}
	if d.current.Img.CodedWidth != 16 || d.current.Img.CodedHeight != 16 {
		t.Fatalf("current coded dimensions = %dx%d, want 16x16", d.current.Img.CodedWidth, d.current.Img.CodedHeight)
	}
	if d.lastRef.BufferLen() == 0 || d.goldenRef.BufferLen() == 0 || d.altRef.BufferLen() == 0 {
		t.Fatalf("reference buffers were not initialized")
	}
	if d.mbRows != 1 || d.mbCols != 1 || len(d.modes) != 1 || len(d.tokens) != 1 || len(d.tokenAbove) != 1 {
		t.Fatalf("workspace rows/cols/lens = %dx%d %d/%d/%d, want 1x1 1/1/1", d.mbRows, d.mbCols, len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
	if d.current.Img.CodedWidth < d.mbCols*16 || d.current.Img.CodedHeight < d.mbRows*16 {
		t.Fatalf("coded frame is smaller than macroblock workspace")
	}
}

func TestDecodeParsesStateAndInitializesDequants(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.previousQuant.BaseQIndex != 0 || d.state.Quant.BaseQIndex != 0 {
		t.Fatalf("quant state = %+v/%+v, want base q 0", d.previousQuant, d.state.Quant)
	}
	if d.dequants[0].Y1[0] != 4 || d.dequants[0].Y2[0] != 8 || d.dequants[0].UV[0] != 4 {
		t.Fatalf("segment 0 dequants = Y1:%d Y2:%d UV:%d, want 4/8/4", d.dequants[0].Y1[0], d.dequants[0].Y2[0], d.dequants[0].UV[0])
	}
}

func TestDecodePersistsCoefficientProbabilityUpdates(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(true, 77))

	err = d.Decode(packet)
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.state.Probability.UpdateCount != 1 || !d.state.Refresh.RefreshEntropyProbs {
		t.Fatalf("probability/refresh = %+v/%t, want one persisted update", d.state.Probability, d.state.Refresh.RefreshEntropyProbs)
	}
	if got := d.frameCoefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("frame coefficient probability = %d, want 77", got)
	}
	if got := d.coefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("persistent coefficient probability = %d, want 77", got)
	}
}

func TestDecodeKeepsTransientCoefficientProbabilityUpdatesFrameLocal(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithFirstPartition(16, 16, vp8FirstPartitionWithSingleCoefProbabilityUpdate(false, 77))

	err = d.Decode(packet)
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.state.Probability.UpdateCount != 1 || d.state.Refresh.RefreshEntropyProbs {
		t.Fatalf("probability/refresh = %+v/%t, want one transient update", d.state.Probability, d.state.Refresh.RefreshEntropyProbs)
	}
	if got := d.frameCoefProbs[0][0][0][0]; got != 77 {
		t.Fatalf("frame coefficient probability = %d, want 77", got)
	}
	if got := d.coefProbs[0][0][0][0]; got != vp8tables.DefaultCoefProbs[0][0][0][0] {
		t.Fatalf("persistent coefficient probability = %d, want default %d", got, vp8tables.DefaultCoefProbs[0][0][0][0])
	}
}

func TestDecodeParsesPartitionLayout(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.partitions.TokenCount != 1 || len(d.partitions.First) != 200 || len(d.partitions.Tokens[0]) == 0 {
		t.Fatalf("partition layout = first:%d tokenCount:%d token0:%d, want nonempty one-partition layout", len(d.partitions.First), d.partitions.TokenCount, len(d.partitions.Tokens[0]))
	}
	if d.frameHeader.FirstPartitionSize != 200 {
		t.Fatalf("frame first partition = %d, want 200", d.frameHeader.FirstPartitionSize)
	}
	if d.modeReader.Err() != nil || d.modeReader.Corrupted() {
		t.Fatalf("mode reader error/corrupted = %v/%v, want clean reader", d.modeReader.Err(), d.modeReader.Corrupted())
	}
}

func vp8KeyFramePacketWithFirstPartition(width int, height int, first []byte) []byte {
	packet := vp8KeyFramePacket(width, height, len(first), 0, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

func vp8FirstPartitionWithSingleCoefProbabilityUpdate(refreshEntropy bool, value uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for i := 0; i < 5; i++ {
		w.writeBool(0, 128)
	}
	if refreshEntropy {
		w.writeBool(1, 128)
	} else {
		w.writeBool(0, 128)
	}

	first := true
	for block := 0; block < vp8tables.BlockTypes; block++ {
		for band := 0; band < vp8tables.CoefBands; band++ {
			for ctx := 0; ctx < vp8tables.PrevCoefContexts; ctx++ {
				for node := 0; node < vp8tables.EntropyNodes; node++ {
					if first {
						w.writeBool(1, vp8tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}

	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

type vp8TestBoolWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *vp8TestBoolWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *vp8TestBoolWriter) writeLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.writeBool(uint8((value>>uint(bit))&1), 128)
	}
}

func (w *vp8TestBoolWriter) finish() []byte {
	for i := 0; i < 32; i++ {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *vp8TestBoolWriter) writeBool(bit uint8, probability uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))

	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(vp8tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}

		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func TestDecodeParsesKeyFrameModeGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(17, 17, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if len(d.modes) != 4 {
		t.Fatalf("modes len = %d, want 4", len(d.modes))
	}
	for i, mode := range d.modes {
		if mode.Mode != vp8common.BPred || mode.UVMode != vp8common.DCPred || !mode.Is4x4 {
			t.Fatalf("mode[%d] = %+v, want keyframe BPred/DC 4x4", i, mode)
		}
		for block, blockMode := range mode.BModes {
			if blockMode != vp8common.BDCPred {
				t.Fatalf("mode[%d].BModes[%d] = %d, want BDCPred", i, block, blockMode)
			}
		}
	}
}

func TestDecodeParsesTokenGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if len(d.tokens) != 1 {
		t.Fatalf("tokens len = %d, want 1", len(d.tokens))
	}
	if d.tokens[0] != (vp8dec.MacroblockTokens{}) {
		t.Fatalf("tokens[0] = %+v, want zero token macroblock", d.tokens[0])
	}
	if d.tokenAbove[0] != (vp8dec.EntropyContextPlanes{}) {
		t.Fatalf("tokenAbove[0] = %+v, want zero context", d.tokenAbove[0])
	}
}

func TestDecodeReconstructsKeyFrameIntraGridInCurrent(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	yChecks := []struct {
		row  int
		col  int
		want byte
	}{
		{row: 0, col: 0, want: 128},
		{row: 4, col: 0, want: 129},
		{row: 15, col: 15, want: 129},
	}
	for _, check := range yChecks {
		if got := d.current.Img.Y[check.row*d.current.Img.YStride+check.col]; got != check.want {
			t.Fatalf("Y[%d,%d] = %d, want %d", check.row, check.col, got, check.want)
		}
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			if got := d.current.Img.U[row*d.current.Img.UStride+col]; got != 128 {
				t.Fatalf("U[%d,%d] = %d, want 128", row, col, got)
			}
			if got := d.current.Img.V[row*d.current.Img.VStride+col]; got != 128 {
				t.Fatalf("V[%d,%d] = %d, want 128", row, col, got)
			}
		}
	}
}

func TestDecodeRefreshesKeyFrameReferences(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("Decode error = %v, want ErrUnsupportedFeature", err)
	}

	if d.lastRef.Img.Y[0] != d.current.Img.Y[0] || d.goldenRef.Img.Y[0] != d.current.Img.Y[0] || d.altRef.Img.Y[0] != d.current.Img.Y[0] {
		t.Fatalf("reference Y[0] values = %d/%d/%d, want current %d", d.lastRef.Img.Y[0], d.goldenRef.Img.Y[0], d.altRef.Img.Y[0], d.current.Img.Y[0])
	}
	if d.lastRef.Img.U[0] != d.current.Img.U[0] || d.goldenRef.Img.V[0] != d.current.Img.V[0] || d.altRef.Img.V[0] != d.current.Img.V[0] {
		t.Fatalf("reference chroma was not refreshed from current")
	}
}

func TestDecodeRejectsMissingTokenPartition(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacket(16, 16, 200, 0, true)
	packet = append(packet, make([]byte, 200)...)

	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeRejectsTruncatedStateHeader(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8KeyFramePacket(16, 16, 200, 0, true))
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("Decode error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeReusesReferenceFrameBuffers(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(16, 16, 200, 0, true)
	_ = d.Decode(packet)
	firstY := &d.current.Img.Y[0]
	firstLastY := &d.lastRef.Img.Y[0]
	firstModes := &d.modes[0]
	firstTokens := &d.tokens[0]

	_ = d.Decode(packet)

	if &d.current.Img.Y[0] != firstY {
		t.Fatalf("current frame buffer was reallocated for same resolution")
	}
	if &d.lastRef.Img.Y[0] != firstLastY {
		t.Fatalf("last reference buffer was reallocated for same resolution")
	}
	if &d.modes[0] != firstModes || &d.tokens[0] != firstTokens {
		t.Fatalf("macroblock workspace was reallocated for same resolution")
	}
}

func TestDecodeWorkspaceTracksMacroblockGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_ = d.Decode(vp8KeyFramePacketWithPayload(17, 17, 200, 0, true))

	if d.mbRows != 2 || d.mbCols != 2 {
		t.Fatalf("workspace grid = %dx%d, want 2x2", d.mbRows, d.mbCols)
	}
	if len(d.modes) != 4 || len(d.tokens) != 4 || len(d.tokenAbove) != 2 {
		t.Fatalf("workspace lengths = %d/%d/%d, want 4/4/2", len(d.modes), len(d.tokens), len(d.tokenAbove))
	}
}

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecoderHotPathAllocs(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	packet := vp8KeyFramePacketWithPayload(64, 64, 200, 0, true)
	dst := Image{Width: 64, Height: 64}

	tests := []struct {
		name string
		fn   func()
	}{
		{name: "Decode", fn: func() { _ = d.Decode(packet) }},
		{name: "DecodeInto", fn: func() { _, _ = d.DecodeInto(packet, &dst) }},
		{name: "NextFrame", fn: func() { _, _ = d.NextFrame() }},
		{name: "Reset", fn: func() { d.Reset() }},
	}

	for _, tt := range tests {
		allocs := testing.AllocsPerRun(1000, tt.fn)
		if allocs != 0 {
			t.Fatalf("%s allocs = %v, want 0", tt.name, allocs)
		}
	}
}
