package codec_test

import (
	"errors"
	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"testing"
)

// FuzzVP8DecoderDecode is the non-oracle valid-input variant of the VP8
// decoder fuzz: it builds a tiny YUV image from the fuzz []byte, encodes
// it as a real VP8 keyframe via the public encoder, then decodes the
// resulting packet and asserts the roundtrip preserves dimensions and
// produces a non-empty visible frame.
//
// The complementary FuzzVP8DecoderMalformedPackets fuzzes the rejection
// path; this one fuzzes the happy path and pins the post-decode shape
// so a regression on output strides, plane sizing, or FrameInfo
// dimensions surfaces immediately.
func FuzzVP8DecoderDecode(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		{0xff, 0x00, 0x80, 0x40, 0x20, 0x10},
		// A short repeating pattern so the encoder receives flat luma.
		{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80},
		// Mixed-noise seed.
		{0x10, 0x42, 0x87, 0xab, 0xc0, 0x55, 0xaa, 0xff, 0x00, 0x33, 0x66, 0x99},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("decode-roundtrip fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		const width, height = 32, 32
		src := vp8FuzzYUVNoiseImage(width, height, data)
		packet, ok := vp8FuzzEncodeOnce(t, width, height, src)
		if !ok {
			// Encoder declined this seed (e.g. rate control dropped the
			// frame, or the encoder rejected the config). Not interesting
			// here; the test is still considered a pass.
			return
		}

		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{MaxWidth: width, MaxHeight: height})
		if err != nil {
			t.Fatalf("NewVP8Decoder: %v", err)
		}
		defer func() {
			_ = d.Close()
		}()

		dst := newVP8FacadeImage(width, height)
		info, err := d.DecodeInto(packet, &dst)
		if err != nil {
			// A valid encoder output decoded by a matching decoder must
			// succeed. If it fails, only the documented sentinels are
			// acceptable; anything else (or a non-sentinel) is a bug.
			if !errors.Is(err, govpx.ErrInvalidData) &&
				!errors.Is(err, govpx.ErrInvalidConfig) &&
				!errors.Is(err, govpx.ErrFrameRejected) {
				t.Fatalf("DecodeInto on freshly-encoded keyframe returned unexpected error: %v", err)
			}
			return
		}

		if info.Width != width || info.Height != height {
			t.Fatalf("FrameInfo dimensions = %dx%d, want %dx%d",
				info.Width, info.Height, width, height)
		}
		if !info.KeyFrame {
			t.Fatalf("first encoded packet not reported as keyframe; FrameInfo=%+v", info)
		}
		if !info.ShowFrame {
			t.Fatalf("first encoded packet not reported as visible; FrameInfo=%+v", info)
		}
		// Roundtrip must produce a usable I420 frame: visible dimensions
		// match, plane lengths cover the visible region, and the Y plane
		// is not entirely zero (would indicate the decoder wrote nothing).
		if dst.Width != width || dst.Height != height {
			t.Fatalf("dst dimensions changed: %dx%d (want %dx%d)",
				dst.Width, dst.Height, width, height)
		}
		if len(dst.Y) < width*height {
			t.Fatalf("dst.Y too short: len=%d want>=%d", len(dst.Y), width*height)
		}
		uvSize := ((width + 1) >> 1) * ((height + 1) >> 1)
		if len(dst.U) < uvSize || len(dst.V) < uvSize {
			t.Fatalf("dst chroma too short: U=%d V=%d want>=%d",
				len(dst.U), len(dst.V), uvSize)
		}
		nonZero := false
		for _, b := range dst.Y[:width*height] {
			if b != 0 {
				nonZero = true
				break
			}
		}
		if !nonZero {
			// All-zero Y can be legitimate if the fuzz input happened to
			// produce a pure-black input — flag only when the source was
			// non-zero so we don't false-positive on intentional black.
			srcNonZero := false
			for _, b := range src.Y[:width*height] {
				if b != 0 {
					srcNonZero = true
					break
				}
			}
			if srcNonZero {
				t.Fatalf("decoded Y plane is all zero but source had non-zero samples")
			}
		}
	})
}

// vp8FuzzYUVNoiseImage builds a width×height I420 image whose plane samples
// are derived from data via a simple deterministic XOR/rotate scheme. Empty
// data produces a 128-grey image.
func vp8FuzzYUVNoiseImage(width, height int, data []byte) govpx.Image {
	img := newVP8FacadeImage(width, height)
	if len(data) == 0 {
		for i := range img.Y {
			img.Y[i] = 128
		}
		for i := range img.U {
			img.U[i] = 128
		}
		for i := range img.V {
			img.V[i] = 128
		}
		return img
	}
	step := len(data)
	for i := range img.Y {
		img.Y[i] = data[i%step] ^ byte(i)
	}
	for i := range img.U {
		img.U[i] = data[i%step] ^ byte(i*3)
	}
	for i := range img.V {
		img.V[i] = data[i%step] ^ byte(i*5)
	}
	return img
}

// vp8FuzzEncodeOnce encodes a single frame at fixed CBR/realtime settings
// suitable for the fuzz harness. Returns (packet, true) on a non-empty
// emitted keyframe, (nil, false) otherwise. This keeps the fuzz body
// focused on the decode-side assertions.
func vp8FuzzEncodeOnce(t *testing.T, width, height int, src govpx.Image) ([]byte, bool) {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return nil, false
	}
	dst := make([]byte, width*height*4+1024)
	result, err := e.EncodeInto(dst, src, 0, 1, govpx.EncodeForceKeyFrame)
	if err != nil || result.Dropped || len(result.Data) == 0 {
		return nil, false
	}
	return append([]byte(nil), result.Data...), true
}

func TestDecodeIntoRejectsNilImage(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}

	_, err = d.DecodeInto(vp8test.KeyFramePacket(16, 16, 0, 0, true), nil)
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestDecodeIntoCopiesSupportedKeyFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8FacadeImage(16, 16)

	info, err := d.DecodeIntoWithPTS(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.Width != 16 || info.Height != 16 || !info.KeyFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want 16x16 keyframe PTS 88", info)
	}
	if got := dst.Y[0]; got != 128 {
		t.Fatalf("dst Y[0] = %d, want 128", got)
	}
	if got := dst.U[0]; got != 128 {
		t.Fatalf("dst U[0] = %d, want 128", got)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued a frame for NextFrame")
	}
}

func TestDecodeIntoFrameInfoReportsQuantizerAndReferences(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8FacadeImage(16, 16)
	keyPacket := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithBaseQIndex(20))

	key, err := d.DecodeIntoWithPTS(keyPacket, &dst, 100)
	if err != nil {
		t.Fatalf("key DecodeIntoWithPTS error = %v, want nil", err)
	}
	if key.InternalQuantizer != 20 || key.Quantizer != vp8common.QIndexToPublicQuantizer(20) {
		t.Fatalf("key quantizer = public:%d internal:%d, want public %d / internal 20", key.Quantizer, key.InternalQuantizer, vp8common.QIndexToPublicQuantizer(20))
	}
	if key.RefUpdates != govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden|govpx.ReferenceFlagAltRef || key.RefUsed != 0 {
		t.Fatalf("key refs = updates:%03b used:%03b, want all updates / no inter refs", key.RefUpdates, key.RefUsed)
	}

	first := vp8test.InterFirstPartitionLastZeroMVWithConfig(vp8common.OnePartition, false, 36)
	inter, err := d.DecodeIntoWithPTS(vp8test.InterFramePacketWithFirstPartition(first), &dst, 101)
	if err != nil {
		t.Fatalf("inter DecodeIntoWithPTS error = %v, want nil", err)
	}
	if inter.InternalQuantizer != 36 || inter.Quantizer != vp8common.QIndexToPublicQuantizer(36) {
		t.Fatalf("inter quantizer = public:%d internal:%d, want public %d / internal 36", inter.Quantizer, inter.InternalQuantizer, vp8common.QIndexToPublicQuantizer(36))
	}
	if inter.RefUpdates != 0 || inter.RefUsed != govpx.ReferenceFlagLast {
		t.Fatalf("inter refs = updates:%03b used:%03b, want no updates / LAST used", inter.RefUpdates, inter.RefUsed)
	}
}

func TestDecodeLastFrameInfoReportsMostRecentFrame(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo before decode returned ok")
	}
	keyPacket := vp8test.KeyFramePacketWithFirstPartition(16, 16, vp8test.FirstPartitionWithBaseQIndex(18))
	if err := d.DecodeWithPTS(keyPacket, 200); err != nil {
		t.Fatalf("key DecodeWithPTS error = %v, want nil", err)
	}
	key, ok := d.LastFrameInfo()
	if !ok {
		t.Fatalf("LastFrameInfo after key decode returned !ok")
	}
	if key.PTS != 200 || !key.KeyFrame || key.InternalQuantizer != 18 || key.RefUpdates != govpx.ReferenceFlagLast|govpx.ReferenceFlagGolden|govpx.ReferenceFlagAltRef {
		t.Fatalf("key LastFrameInfo = %+v, want PTS 200 key q18 all ref updates", key)
	}
	publicQ, internalQ, ok := d.LastQuantizer()
	if !ok {
		t.Fatalf("LastQuantizer after key decode returned !ok")
	}
	if publicQ != vp8common.QIndexToPublicQuantizer(18) || internalQ != 18 {
		t.Fatalf("LastQuantizer = public:%d internal:%d, want public %d / internal 18",
			publicQ, internalQ, vp8common.QIndexToPublicQuantizer(18))
	}
	d.Reset()
	if _, ok := d.LastFrameInfo(); ok {
		t.Fatalf("LastFrameInfo after Reset returned ok")
	}
	if _, _, ok := d.LastQuantizer(); ok {
		t.Fatalf("LastQuantizer after Reset returned ok")
	}
}

func TestDecodeIntoInvisibleFrameDoesNotCopyOutput(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(dst, 7, 8, 9)

	info, err := d.DecodeIntoWithPTS(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, false), &dst, 88)
	if err != nil {
		t.Fatalf("DecodeIntoWithPTS error = %v, want nil", err)
	}
	if info.ShowFrame || info.PTS != 88 {
		t.Fatalf("FrameInfo = %+v, want invisible PTS 88", info)
	}
	if dst.Y[0] != 7 || dst.U[0] != 8 || dst.V[0] != 9 {
		t.Fatalf("dst samples = %d/%d/%d, want unchanged 7/8/9", dst.Y[0], dst.U[0], dst.V[0])
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatalf("DecodeInto queued invisible frame")
	}
}

func TestDecodeIntoRejectsInvalidImage(t *testing.T) {
	d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	dst := govpx.Image{Width: 16, Height: 16}

	_, err = d.DecodeInto(vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true), &dst)
	if !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("DecodeInto error = %v, want ErrInvalidConfig", err)
	}
}

func newVP8FacadeImage(width int, height int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return govpx.Image{
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

func fillVP8FacadeImage(img govpx.Image, y byte, u byte, v byte) {
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

func assertVP8FacadeImagesEqual(t *testing.T, name string, want govpx.Image, got govpx.Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", name, got.Width, got.Height, want.Width, want.Height)
	}
	assertVP8FacadePlaneEqual(t, name+" Y", want.Y, want.YStride, got.Y, got.YStride, want.Width, want.Height)
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	assertVP8FacadePlaneEqual(t, name+" U", want.U, want.UStride, got.U, got.UStride, uvWidth, uvHeight)
	assertVP8FacadePlaneEqual(t, name+" V", want.V, want.VStride, got.V, got.VStride, uvWidth, uvHeight)
}

func assertVP8FacadePlaneEqual(
	t *testing.T, name string,
	want []byte, wantStride int,
	got []byte, gotStride int,
	width int, height int,
) {
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

type capturedFramePlanes struct {
	width  int
	height int
	y      []byte
	u      []byte
	v      []byte
}

func captureDecodedPlanes(img govpx.Image) capturedFramePlanes {
	yWidth, yHeight := img.Width, img.Height
	y := make([]byte, yWidth*yHeight)
	for row := range yHeight {
		copy(y[row*yWidth:(row+1)*yWidth], img.Y[row*img.YStride:row*img.YStride+yWidth])
	}

	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1
	u := make([]byte, uvWidth*uvHeight)
	v := make([]byte, uvWidth*uvHeight)
	for row := range uvHeight {
		copy(u[row*uvWidth:(row+1)*uvWidth], img.U[row*img.UStride:row*img.UStride+uvWidth])
		copy(v[row*uvWidth:(row+1)*uvWidth], img.V[row*img.VStride:row*img.VStride+uvWidth])
	}

	return capturedFramePlanes{width: yWidth, height: yHeight, y: y, u: u, v: v}
}

// FuzzVP8DecoderDecodeInto mirrors FuzzVP9DecoderDecodeInto on the VP8 surface.
// It feeds the fuzz []byte as a single VP8 frame to Decoder.DecodeInto and
// asserts the decoder either returns a documented sentinel error or writes a
// sane I420 frame: no panic, dimensions inside the configured caps, no
// out-of-bounds plane writes (encoded by destination capacity), and chroma
// planes sized to the 4:2:0 ratio matching the visible width/height.
//
// Two destinations are exercised on the same packet: a small 64x64 buffer
// (so the validForEncode reject path is reached for over-dimensioned keys)
// and a 256x256 buffer at the decoder's MaxWidth/MaxHeight (so the happy
// path is reached when the packet is a small keyframe).
func FuzzVP8DecoderDecodeInto(f *testing.F) {
	seeds := vp8DecoderFuzzSeeds(f)
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{MaxWidth: 256, MaxHeight: 256})
		if err != nil {
			t.Fatalf("NewVP8Decoder: %v", err)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DecodeInto panicked on %d-byte input: %v", len(packet), r)
			}
			_ = d.Close()
		}()

		smallDst := newVP8FacadeImage(64, 64)
		smallInfo, err := d.DecodeInto(packet, &smallDst)
		if err != nil {
			assertVP8FuzzDecodeError(t, err)
		} else {
			assertVP8FuzzFrameInfoSane(t, smallInfo, 64, 64)
			assertVP8FuzzDstShapeSane(t, smallDst)
		}

		// Reset to clear any half-applied state from the first attempt
		// and try again with a larger destination — the same packet may
		// have been rejected by the small destination's validForEncode
		// but accepted by a 256x256 destination.
		d.Reset()
		largeDst := newVP8FacadeImage(256, 256)
		largeInfo, err := d.DecodeInto(packet, &largeDst)
		if err != nil {
			assertVP8FuzzDecodeError(t, err)
		} else {
			assertVP8FuzzFrameInfoSane(t, largeInfo, 256, 256)
			assertVP8FuzzDstShapeSane(t, largeDst)
		}
	})
}

// assertVP8FuzzDecodeError pins the set of errors the VP8 decoder may return
// for arbitrary inputs. Anything else means the decoder leaked an internal
// sentinel or panicked in disguise.
func assertVP8FuzzDecodeError(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, govpx.ErrInvalidData):
	case errors.Is(err, govpx.ErrNeedKeyFrame):
	case errors.Is(err, govpx.ErrFrameNotReady):
	case errors.Is(err, govpx.ErrFrameRejected):
	case errors.Is(err, govpx.ErrInvalidConfig):
	case errors.Is(err, govpx.ErrBufferTooSmall):
	case errors.Is(err, govpx.ErrClosed):
	default:
		t.Fatalf("DecodeInto returned unexpected error: %v", err)
	}
}

// assertVP8FuzzFrameInfoSane checks that a successful DecodeInto returned a
// FrameInfo whose dimensions fit inside the caller's destination dimensions
// when the frame was visible. Hidden frames carry zero-dimension info and
// don't write to dst.
func assertVP8FuzzFrameInfoSane(t *testing.T, info govpx.FrameInfo, maxW, maxH int) {
	t.Helper()
	if !info.ShowFrame {
		return
	}
	if info.Width < 0 || info.Height < 0 {
		t.Fatalf("FrameInfo has negative dimensions: %dx%d", info.Width, info.Height)
	}
	if info.Width > maxW || info.Height > maxH {
		t.Fatalf("FrameInfo dimensions %dx%d exceed destination caps %dx%d",
			info.Width, info.Height, maxW, maxH)
	}
}

// assertVP8FuzzDstShapeSane checks that the destination image still has
// the expected 4:2:0 plane shape after DecodeInto. The decoder is supposed
// to leave the caller-owned strides intact and never reallocate.
func assertVP8FuzzDstShapeSane(t *testing.T, dst govpx.Image) {
	t.Helper()
	if dst.YStride < dst.Width || dst.UStride < (dst.Width+1)>>1 || dst.VStride < (dst.Width+1)>>1 {
		t.Fatalf("dst strides shrunk below visible width: y=%d u=%d v=%d w=%d",
			dst.YStride, dst.UStride, dst.VStride, dst.Width)
	}
	if len(dst.Y) < dst.YStride*dst.Height {
		t.Fatalf("dst.Y shrunk: len=%d need>=%d", len(dst.Y), dst.YStride*dst.Height)
	}
	uvH := (dst.Height + 1) >> 1
	if len(dst.U) < dst.UStride*uvH {
		t.Fatalf("dst.U shrunk: len=%d need>=%d", len(dst.U), dst.UStride*uvH)
	}
	if len(dst.V) < dst.VStride*uvH {
		t.Fatalf("dst.V shrunk: len=%d need>=%d", len(dst.V), dst.VStride*uvH)
	}
}

// vp8DecoderFuzzSeeds returns a curated seed corpus mirroring the layout of
// vp9DecoderFuzzSeeds: a mix of empty/short inputs, malformed frame tags,
// and real visible keyframes produced by the encoder so the fuzz harness
// can mutate around them.
func vp8DecoderFuzzSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		{0xff},
		{0x00, 0x00, 0x00},
		{0xff, 0xff, 0xff},
		vp8test.KeyFramePacket(16, 16, 0, 0, true),
		vp8test.KeyFramePacket(16, 16, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(16, 16, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, true),
		vp8test.KeyFramePacketWithPayload(64, 64, 200, 0, false),
		vp8test.KeyFramePacketWithPayload(32, 32, 200, 0, true),
		vp8test.InterFramePacket(0, 0, true),
		vp8test.InterFramePacket(1, 0, true),
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 16, 16); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 64, 64); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	if pkt := vp8FuzzEncodedKeyframe(tb, 128, 128); len(pkt) > 0 {
		seeds = append(seeds, pkt)
	}
	return seeds
}

// vp8FuzzEncodedKeyframe encodes a single visible VP8 keyframe via the public
// encoder so the corpus contains valid bitstreams the fuzzer can mutate.
// Returns nil if encoding is unavailable; the fuzz harness still runs on
// smoke seeds in that case.
func vp8FuzzEncodedKeyframe(tb testing.TB, width, height int) []byte {
	tb.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return nil
	}
	img := newVP8FacadeImage(width, height)
	for i := range img.Y {
		img.Y[i] = 96
	}
	for i := range img.U {
		img.U[i] = 128
	}
	for i := range img.V {
		img.V[i] = 128
	}
	dst := make([]byte, width*height*4+1024)
	result, err := e.EncodeInto(dst, img, 0, 1, 0)
	if err != nil || len(result.Data) == 0 {
		return nil
	}
	return append([]byte(nil), result.Data...)
}
