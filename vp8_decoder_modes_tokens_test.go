package govpx

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
)

func TestDecodeParsesKeyFrameModeGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(17, 17, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
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

func TestDecodeParsesInterModeGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	fillVP8Image(&d.lastRef.Img, 77)
	packet := vp8test.InterFramePacketWithFirstPartition(vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 0))

	err = d.Decode(packet)
	if err != nil {
		t.Fatalf("inter Decode error = %v, want nil", err)
	}
	if len(d.modes) != 1 {
		t.Fatalf("modes len = %d, want 1", len(d.modes))
	}
	if d.modes[0].RefFrame != vp8common.LastFrame || d.modes[0].Mode != vp8common.ZeroMV || !d.modes[0].MV.IsZero() {
		t.Fatalf("inter mode = %+v, want LAST/ZEROMV", d.modes[0])
	}
	for block, coeffs := range d.tokens[0].QCoeff {
		if coeffs != ([16]int16{}) {
			t.Fatalf("tokens[0].QCoeff[%d] = %v, want zero coefficients", block, coeffs)
		}
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no inter frame")
	}
	if frame.Width != 16 || frame.Height != 16 {
		t.Fatalf("inter frame dimensions = %dx%d, want 16x16", frame.Width, frame.Height)
	}
	if frame.Y[0] != 77 || frame.U[0] != 77 || frame.V[0] != 77 {
		t.Fatalf("inter frame samples = %d/%d/%d, want copied last ref 77", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestDecodeErrorConcealmentClampsUnusedMalformedTokenPartition(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{ErrorConcealment: true})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}
	fillVP8Image(&d.lastRef.Img, 77)
	first := vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.TwoPartition, true, 0)
	packet := vp8test.InterFramePacketWithTokenPartitions(first, 10, []byte{0})

	err = d.DecodeWithPTS(packet, 202)
	if err != nil {
		t.Fatalf("inter DecodeWithPTS error = %v, want nil", err)
	}
	if d.lastInfo.Corrupted || d.lastInfo.PTS != 202 {
		t.Fatalf("lastInfo = %+v, want clean decoded inter frame PTS 202", d.lastInfo)
	}
	if d.partitions.TokenCount != 2 || len(d.partitions.Tokens[0]) != 1 || len(d.partitions.Tokens[1]) != 0 {
		t.Fatalf("token partitions = count:%d lens:%d/%d, want clamped one-byte first and empty unused second", d.partitions.TokenCount, len(d.partitions.Tokens[0]), len(d.partitions.Tokens[1]))
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no inter frame")
	}
	if frame.Y[0] != 77 || frame.U[0] != 77 || frame.V[0] != 77 {
		t.Fatalf("inter frame samples = %d/%d/%d, want copied last ref 77", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestDecodeRejectsMalformedTokenPartitionWhenConcealmentDisabled(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true)); err != nil {
		t.Fatalf("keyframe Decode error = %v, want nil", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatalf("keyframe NextFrame returned no frame")
	}
	first := vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.TwoPartition, true, 0)
	packet := vp8test.InterFramePacketWithTokenPartitions(first, 10, []byte{0})

	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidData) {
		t.Fatalf("inter Decode error = %v, want ErrInvalidData", err)
	}
}

func TestDecodeParsesTokenGrid(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
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

	err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
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
	for row := range 8 {
		for col := range 8 {
			if got := d.current.Img.U[row*d.current.Img.UStride+col]; got != 128 {
				t.Fatalf("U[%d,%d] = %d, want 128", row, col, got)
			}
			if got := d.current.Img.V[row*d.current.Img.VStride+col]; got != 128 {
				t.Fatalf("V[%d,%d] = %d, want 128", row, col, got)
			}
		}
	}
}

func TestDecodeExtendsKeyFrameCodedBorders(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(5, 3, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	assertCodedBordersExtended(t, &d.current.Img)
	assertCodedBordersExtended(t, &d.lastRef.Img)
	assertCodedBordersExtended(t, &d.goldenRef.Img)
	assertCodedBordersExtended(t, &d.altRef.Img)
}

func TestDecodeRefreshesKeyFrameReferences(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	err = d.Decode(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true))
	if err != nil {
		t.Fatalf("Decode error = %v, want nil", err)
	}

	if d.lastRef.Img.Y[0] != d.current.Img.Y[0] || d.goldenRef.Img.Y[0] != d.current.Img.Y[0] || d.altRef.Img.Y[0] != d.current.Img.Y[0] {
		t.Fatalf("reference Y[0] values = %d/%d/%d, want current %d", d.lastRef.Img.Y[0], d.goldenRef.Img.Y[0], d.altRef.Img.Y[0], d.current.Img.Y[0])
	}
	if d.lastRef.Img.U[0] != d.current.Img.U[0] || d.goldenRef.Img.V[0] != d.current.Img.V[0] || d.altRef.Img.V[0] != d.current.Img.V[0] {
		t.Fatalf("reference chroma was not refreshed from current")
	}
}

func TestRefreshReferencesCopiesExistingBuffersInVP8Order(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	fillVP8Image(&d.current.Img, 40)
	fillVP8Image(&d.lastRef.Img, 10)
	fillVP8Image(&d.goldenRef.Img, 20)
	fillVP8Image(&d.altRef.Img, 30)
	d.state.Refresh = vp8dec.RefreshHeader{
		CopyBufferToAltRef: 2,
		CopyBufferToGolden: 1,
	}

	d.refreshReferences()

	if got := d.altRef.Img.Y[0]; got != 20 {
		t.Fatalf("alt Y[0] = %d, want old golden 20", got)
	}
	if got := d.goldenRef.Img.Y[0]; got != 10 {
		t.Fatalf("golden Y[0] = %d, want last 10", got)
	}
	if got := d.lastRef.Img.Y[0]; got != 10 {
		t.Fatalf("last Y[0] = %d, want unchanged 10", got)
	}
}

func TestRefreshReferencesCurrentFrameOverridesCopies(t *testing.T) {
	d, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.ensureFrameBuffers(StreamInfo{Width: 16, Height: 16, KeyFrame: true}); err != nil {
		t.Fatalf("ensureFrameBuffers returned error: %v", err)
	}
	fillVP8Image(&d.current.Img, 40)
	fillVP8Image(&d.lastRef.Img, 10)
	fillVP8Image(&d.goldenRef.Img, 20)
	fillVP8Image(&d.altRef.Img, 30)
	d.state.Refresh = vp8dec.RefreshHeader{
		CopyBufferToGolden: 1,
		RefreshGolden:      true,
		RefreshAltRef:      true,
		RefreshLast:        true,
	}

	d.refreshReferences()

	if d.goldenRef.Img.Y[0] != 40 || d.altRef.Img.Y[0] != 40 || d.lastRef.Img.Y[0] != 40 {
		t.Fatalf("reference Y[0] = %d/%d/%d, want all current 40", d.goldenRef.Img.Y[0], d.altRef.Img.Y[0], d.lastRef.Img.Y[0])
	}
}
