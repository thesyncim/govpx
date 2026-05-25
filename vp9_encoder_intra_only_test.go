package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderEncodeIntraOnlyFrameRefreshesLastAndShowExisting(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 16, 128, 128)
	src := vp9test.NewYCbCr(width, height, 83, 141, 209)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeIntraOnlyFrame(src, 0)
	if err != nil {
		t.Fatalf("EncodeIntraOnlyFrame: %v", err)
	}
	info, err := PeekVP9StreamInfo(hidden)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo hidden intra-only: %v", err)
	}
	if info.KeyFrame || !info.IntraOnly || info.ShowFrame ||
		info.RefreshFrameFlags != 1<<vp9LastRefSlot ||
		info.Width != width || info.Height != height {
		t.Fatalf("hidden intra-only info = %+v, want hidden LAST intra-only", info)
	}
	var br vp9dec.BitReader
	br.Init(hidden)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader hidden intra-only: %v", err)
	}
	if hdr.ResetFrameContext != 2 || !hdr.FrameParallelDecoding {
		t.Fatalf("hidden intra-only context flags = reset:%d parallel:%t, want reset 2 and frame-parallel",
			hdr.ResetFrameContext, hdr.FrameParallelDecoding)
	}
	show, err := e.EncodeShowExistingFrame(vp9LastRefSlot)
	if err != nil {
		t.Fatalf("EncodeShowExistingFrame LAST: %v", err)
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after keyframe")
	}
	if err := d.DecodeWithPTS(hidden, 10); err != nil {
		t.Fatalf("Decode hidden intra-only: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden intra-only frame")
	}
	if last, ok := d.LastFrameInfo(); !ok || last.KeyFrame || last.ShowFrame ||
		last.RefreshFrameFlags != 1<<vp9LastRefSlot || last.PTS != 10 {
		t.Fatalf("LastFrameInfo hidden intra-only = %+v ok=%t, want hidden LAST refresh",
			last, ok)
	}
	if err := d.DecodeWithPTS(show, 11); err != nil {
		t.Fatalf("Decode show-existing LAST: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after show-existing LAST")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 83, 141, 209, 1)
}

// TestVP9EncoderIntraOnlyFrameUsesTxModeSelect pins the libvpx-faithful
// intra-only tx_mode dispatch. libvpx's select_tx_mode predicate at
// vp9/encoder/vp9_encodeframe.c:4336 reads `cm->frame_type == KEY_FRAME`
// literally; intra-only frames carry cm->frame_type == INTER_FRAME, so
// the KEY_FRAME && use_nonrd_pick_mode ALLOW_16X16 branch does not fire
// and the dispatch falls through to sf.tx_size_search_method. At the
// govpx default (RT cpu_used=8) the per-frame SF refresh picks
// USE_TX_8X8 (vp9_speed_features.c:1541 — is_keyframe=0 for intra-only),
// which select_tx_mode at vp9_encodeframe.c:4341-4342 returns as
// TX_MODE_SELECT.
//
// The intra-only path must plumb the TxProbs row through the
// vp9ModeTreeKeyframe counts-collection dispatch in writeVP9ModeBlock; this
// test exercises the full encode -> decode roundtrip.

func TestVP9EncoderIntraOnlyFrameUsesTxModeSelect(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	keySrc := vp9test.NewYCbCr(width, height, 96, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 64, 128, 128)
	intra, err := e.EncodeIntraOnlyFrame(src, 0)
	if err != nil {
		t.Fatalf("EncodeIntraOnlyFrame: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(intra)
	intraHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader intra-only: %v", err)
	}
	if !intraHeader.IntraOnly || intraHeader.FrameType != common.InterFrame {
		t.Fatalf("intra-only header = (FrameType=%d, IntraOnly=%t), want "+
			"(InterFrame, true)", intraHeader.FrameType, intraHeader.IntraOnly)
	}
	uncSize := keyBR.BytesRead()
	compEnd := uncSize + int(intraHeader.FirstPartitionSize)
	if compEnd > len(intra) {
		t.Fatalf("compressed header end %d past frame %d", compEnd, len(intra))
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	var cr bitstream.Reader
	if err := cr.Init(intra[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	out := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             false,
		IntraOnly:            true,
		KeyFrame:             false,
		InterpFilter:         intraHeader.InterpFilter,
		AllowHighPrecisionMv: intraHeader.AllowHighPrecisionMv,
		CompoundRefAllowed:   false,
	})
	if out.TxMode != common.TxModeSelect {
		t.Fatalf("intra-only TxMode = %d, want TxModeSelect (libvpx "+
			"vp9_encodeframe.c:4341-4342 USE_TX_8X8 -> TX_MODE_SELECT)",
			out.TxMode)
	}
}

func TestVP9EncoderEncodeIntraOnlyFrameRejectsConflictingFlags(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto before stream init error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst, EncodeForceKeyFrame); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto force-key error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, dst,
		EncodeNoUpdateLast|EncodeNoUpdateGolden|EncodeNoUpdateAltRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("EncodeIntraOnlyFrameInto no-refresh error = %v, want ErrInvalidConfig", err)
	}
	if _, err := e.EncodeIntraOnlyFrameInto(src, nil, 0); !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("EncodeIntraOnlyFrameInto nil dst error = %v, want ErrBufferTooSmall", err)
	}
}
