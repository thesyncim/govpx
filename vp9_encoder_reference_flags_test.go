package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderEncodeIntoWithFlagsNoUpdateLast(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyRefY := e.refFrames[vp9LastRefSlot].img.Y[0]
	interSrc := vp9test.NewYCbCr(width, height, 160, 128, 128)
	dst := make([]byte, 65536)
	n, err := e.EncodeIntoWithFlags(interSrc, dst, EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("EncodeIntoWithFlags no-update-LAST: %v", err)
	}

	var br vp9dec.BitReader
	br.Init(dst[:n])
	refDims := func(slot uint8) (uint32, uint32) {
		if slot > vp9AltRefSlot {
			t.Fatalf("inter header requested ref slot %d, want <= %d", slot, vp9AltRefSlot)
		}
		return width, height
	}
	h, perr := vp9dec.ReadUncompressedHeader(&br, nil, refDims)
	if perr != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", perr)
	}
	if h.FrameType != common.InterFrame {
		t.Fatalf("frame type = %d, want InterFrame", h.FrameType)
	}
	if h.InterRef.RefIndex != [3]uint8{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		t.Fatalf("RefIndex = %v, want LAST/GOLDEN/ALTREF slots 0/1/2", h.InterRef.RefIndex)
	}
	if h.RefreshFrameFlags != 0x06 {
		t.Fatalf("RefreshFrameFlags = %#x, want GOLDEN|ALTREF", h.RefreshFrameFlags)
	}
	if !e.refFrames[0].valid {
		t.Fatal("LAST ref became invalid after no-update-LAST")
	}
	if got := e.refFrames[0].img.Y[0]; got != keyRefY {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keyRefY)
	}
	for _, slot := range []int{vp9GoldenRefSlot, vp9AltRefSlot} {
		if got := e.refFrames[slot].img.Y[0]; got == keyRefY {
			t.Fatalf("ref slot %d Y[0] still has keyframe value %d", slot, got)
		}
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceGoldenAltRefRefreshesSlots(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}

	interSrc := vp9test.NewYCbCr(width, height, 160, 96, 224)
	packet, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeForceAltRefFrame)
	if err != nil {
		t.Fatalf("EncodeWithFlags force GF/ARF: %v", err)
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.RefreshFrameFlags != 0x07 {
		t.Fatalf("RefreshFrameFlags = %#x, want LAST|GOLDEN|ALTREF", info.RefreshFrameFlags)
	}
	for _, slot := range []int{vp9LastRefSlot, vp9GoldenRefSlot, vp9AltRefSlot} {
		if !e.refValid[slot] || !e.refFrames[slot].valid {
			t.Fatalf("reference slot %d was not refreshed", slot)
		}
	}
	if got := e.refFrames[vp9GoldenRefSlot].img.Y[0]; got == keySrc.Y[0] {
		t.Fatalf("GOLDEN ref Y[0] still has keyframe value %d", got)
	}
	if got := e.refFrames[vp9AltRefSlot].img.Y[0]; got == keySrc.Y[0] {
		t.Fatalf("ALTREF ref Y[0] still has keyframe value %d", got)
	}
}

func TestVP9EncoderInterReferenceMaskPrunesAliasedRefreshSlots(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	if e.refMap[vp9LastRefSlot] == 0 ||
		e.refMap[vp9LastRefSlot] != e.refMap[vp9GoldenRefSlot] ||
		e.refMap[vp9LastRefSlot] != e.refMap[vp9AltRefSlot] {
		t.Fatalf("keyframe ref map = %v, want all slots aliased", e.refMap)
	}
	if got, want := e.vp9InterReferenceMaskForFrame(0),
		uint8(1<<uint(vp9dec.LastFrame)); got != want {
		t.Fatalf("implicit inter reference mask = %#x, want LAST only %#x",
			got, want)
	}

	explicitNoLast := EncodeNoReferenceLast
	if got, want := e.vp9InterReferenceMaskForFrame(explicitNoLast),
		uint8(1<<uint(vp9dec.GoldenFrame)|1<<uint(vp9dec.AltrefFrame)); got != want {
		t.Fatalf("explicit no-LAST reference mask = %#x, want %#x", got, want)
	}
}

func TestVP9EncoderSetReferenceFrameBreaksReferenceAlias(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	golden := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(width, height, 192, 128, 128))
	if err := e.SetReferenceFrame(ReferenceGolden, golden); err != nil {
		t.Fatalf("SetReferenceFrame GOLDEN: %v", err)
	}
	if e.refMap[vp9GoldenRefSlot] == e.refMap[vp9LastRefSlot] {
		t.Fatalf("GOLDEN ref map still aliases LAST: %v", e.refMap)
	}
	want := uint8(1<<uint(vp9dec.LastFrame) | 1<<uint(vp9dec.GoldenFrame))
	if got := e.vp9InterReferenceMaskForFrame(0); got != want {
		t.Fatalf("reference mask after external GOLDEN = %#x, want %#x",
			got, want)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceGoldenCanSkipLastUpdate(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	keyRefY := e.refFrames[vp9LastRefSlot].img.Y[0]

	interSrc := vp9test.NewYCbCr(width, height, 196, 96, 224)
	packet, err := e.EncodeWithFlags(interSrc, EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("EncodeWithFlags force GF/no-update-LAST: %v", err)
	}
	info, err := PeekVP9StreamInfo(packet)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.RefreshFrameFlags != 0x06 {
		t.Fatalf("RefreshFrameFlags = %#x, want GOLDEN|ALTREF", info.RefreshFrameFlags)
	}
	if got := e.refFrames[vp9LastRefSlot].img.Y[0]; got != keyRefY {
		t.Fatalf("LAST ref Y[0] = %d, want prior keyframe value %d", got, keyRefY)
	}
	if got := e.refFrames[vp9GoldenRefSlot].img.Y[0]; got == keyRefY {
		t.Fatalf("GOLDEN ref Y[0] still has keyframe value %d", got)
	}
	if got := e.refFrames[vp9AltRefSlot].img.Y[0]; got == keyRefY {
		t.Fatalf("ALTREF ref Y[0] still has keyframe value %d", got)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsForceClearsSameSlotNoUpdate(t *testing.T) {
	const width, height = 64, 64
	tests := []struct {
		name        string
		flags       EncodeFlags
		wantRefresh uint8
		wantSlot    int
	}{
		{
			name:        "golden",
			flags:       EncodeForceGoldenFrame | EncodeNoUpdateGolden | EncodeNoUpdateLast,
			wantRefresh: 0x06,
			wantSlot:    vp9GoldenRefSlot,
		},
		{
			name:        "altref",
			flags:       EncodeForceAltRefFrame | EncodeNoUpdateAltRef | EncodeNoUpdateGolden,
			wantRefresh: 0x05,
			wantSlot:    vp9AltRefSlot,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
			keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
			if _, err := e.Encode(keySrc); err != nil {
				t.Fatalf("Encode keyframe: %v", err)
			}
			keyRefY := e.refFrames[tt.wantSlot].img.Y[0]

			interSrc := vp9test.NewYCbCr(width, height, 196, 96, 224)
			packet, err := e.EncodeWithFlags(interSrc, tt.flags)
			if err != nil {
				t.Fatalf("EncodeWithFlags(%#x): %v", tt.flags, err)
			}
			info, err := PeekVP9StreamInfo(packet)
			if err != nil {
				t.Fatalf("PeekVP9StreamInfo: %v", err)
			}
			if info.RefreshFrameFlags != tt.wantRefresh {
				t.Fatalf("RefreshFrameFlags = %#x, want %#x", info.RefreshFrameFlags,
					tt.wantRefresh)
			}
			if got := e.refFrames[tt.wantSlot].img.Y[0]; got == keyRefY {
				t.Fatalf("forced reference slot %d still has keyframe value %d",
					tt.wantSlot, got)
			}
		})
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceLastMasksLastBlocks(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	goldenSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	goldenRefresh, err := e.EncodeWithFlags(goldenSrc,
		EncodeForceGoldenFrame|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode force-GOLDEN: %v", err)
	}
	inter, err := e.EncodeWithFlags(goldenSrc,
		EncodeNoReferenceLast|EncodeNoReferenceAltRef|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode GOLDEN-only inter: %v", err)
	}
	info, err := PeekVP9StreamInfo(inter)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.KeyFrame {
		t.Fatal("NoReferenceLast forced a keyframe despite usable GOLDEN")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for _, packet := range [][]byte{key, goldenRefresh, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet: %v", err)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after GOLDEN-only inter")
	}
	assertVP9DecodedGridAvoidsReferences(t, d.miGrid, vp9dec.LastFrame,
		vp9dec.AltrefFrame)
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceAllStaysInterIntra(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 72, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := vp9test.NewYCbCr(width, height, 144, 96, 224)
	inter, err := e.EncodeWithFlags(interSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode no-reference-all inter: %v", err)
	}
	header, _ := vp9test.ParseHeader(t, inter)
	if header.FrameType != common.InterFrame || header.IntraOnly {
		t.Fatalf("no-reference-all header frame_type=%d intra_only=%t, want inter/intra-coded blocks",
			header.FrameType, header.IntraOnly)
	}
	if header.RefreshFrameFlags != 1<<vp9LastRefSlot {
		t.Fatalf("no-reference-all refresh = %#x, want LAST refresh",
			header.RefreshFrameFlags)
	}
	if header.InterpFilter != vp9dec.InterpSwitchable {
		t.Fatalf("no-reference-all interp filter = %d, want switchable",
			header.InterpFilter)
	}

	d := decodeVP9KeyInterForTest(t, key, inter)
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after no-reference-all inter")
	}
	if got := d.miGrid[0]; got.RefFrame != [2]int8{vp9dec.IntraFrame, vp9dec.NoRefFrame} {
		t.Fatalf("top-left block ref = %v mode=%d, want intra block inside inter frame",
			got.RefFrame, got.Mode)
	}
}

func TestVP9EncoderEncodeIntoWithFlagsNoReferenceLastGoldenMasksLastAndGoldenBlocks(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	altSrc := vp9test.NewYCbCr(width, height, 44, 208, 96)
	altRefresh, err := e.EncodeWithFlags(altSrc,
		EncodeForceAltRefFrame|EncodeNoUpdateLast|EncodeNoUpdateGolden)
	if err != nil {
		t.Fatalf("Encode force-ALTREF: %v", err)
	}
	inter, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode ALTREF-only inter: %v", err)
	}
	info, err := PeekVP9StreamInfo(inter)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo: %v", err)
	}
	if info.KeyFrame {
		t.Fatal("NoReferenceLast|NoReferenceGolden forced a keyframe despite usable ALTREF")
	}

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	for _, packet := range [][]byte{key, altRefresh, inter} {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet: %v", err)
		}
	}
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty after ALTREF-only inter")
	}
	assertVP9DecodedGridAvoidsReferences(t, d.miGrid, vp9dec.LastFrame,
		vp9dec.GoldenFrame)
}

func assertVP9DecodedGridAvoidsReferences(t *testing.T, grid []vp9dec.NeighborMi,
	forbidden ...int8,
) {
	t.Helper()
	for miIdx, mi := range grid {
		for _, ref := range mi.RefFrame {
			if ref == vp9dec.NoRefFrame || ref == vp9dec.IntraFrame {
				continue
			}
			for _, blocked := range forbidden {
				if ref == blocked {
					t.Fatalf("MI %d used forbidden reference %d: mode=%d refs=%v mv=%+v",
						miIdx, blocked, mi.Mode, mi.RefFrame, mi.Mv[0])
				}
			}
		}
	}
}

func TestVP9EncoderEncodeIntoWithFlagsInvisibleKeyFrameUpdatesReferences(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 91, 143, 37)
	hidden, err := e.EncodeWithFlags(src, EncodeInvisibleFrame)
	if err != nil {
		t.Fatalf("Encode hidden keyframe: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, hidden)
	if h.FrameType != common.KeyFrame || h.ShowFrame {
		t.Fatalf("hidden key header frame_type=%d show=%t, want key/show=false",
			h.FrameType, h.ShowFrame)
	}

	visible, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode visible inter after hidden keyframe: %v", err)
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode hidden keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden keyframe")
	}
	if info, ok := d.LastFrameInfo(); !ok || !info.KeyFrame || info.ShowFrame {
		t.Fatalf("LastFrameInfo after hidden keyframe = %+v ok=%t, want hidden keyframe",
			info, ok)
	}
	if err := d.Decode(visible); err != nil {
		t.Fatalf("Decode visible inter: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible inter")
	}
	assertVP9FilledFrameWithin(t, frame, width, height, 91, 143, 37, 1)
}

func TestVP9EncoderEncodeIntoWithFlagsInvisibleAltRefRefresh(t *testing.T) {
	const width, height = 64, 64
	// CpuUsed: -3 retains the speed=3 picker (full mode/MV search). The
	// default speed=8 path uses VAR_BASED_PARTITION which commits root SB
	// size and skews the per-block reconstruction luma slightly off the
	// expected 188 anchor.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})
	keySrc := vp9test.NewYCbCr(width, height, 64, 128, 128)
	altSrc := vp9test.NewYCbCr(width, height, 188, 96, 224)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	hidden, err := e.EncodeWithFlags(altSrc,
		EncodeInvisibleFrame|EncodeForceAltRefFrame|EncodeNoUpdateLast|
			EncodeNoUpdateGolden|EncodeNoReferenceGolden|EncodeNoReferenceAltRef)
	if err != nil {
		t.Fatalf("Encode hidden altref refresh: %v", err)
	}
	visible, err := e.EncodeWithFlags(altSrc,
		EncodeNoReferenceLast|EncodeNoReferenceGolden|EncodeNoUpdateLast)
	if err != nil {
		t.Fatalf("Encode visible altref-only inter: %v", err)
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
	if err := d.Decode(hidden); err != nil {
		t.Fatalf("Decode hidden altref refresh: %v", err)
	}
	if _, ok := d.NextFrame(); ok {
		t.Fatal("NextFrame returned visible output after hidden altref refresh")
	}
	if info, ok := d.LastFrameInfo(); !ok || info.ShowFrame ||
		info.RefreshFrameFlags != 1<<vp9AltRefSlot {
		t.Fatalf("LastFrameInfo after hidden altref = %+v ok=%t, want hidden ALTREF refresh",
			info, ok)
	}
	if err := d.Decode(visible); err != nil {
		t.Fatalf("Decode visible altref-only inter: %v", err)
	}
	if got := d.miGrid[0]; got.RefFrame[0] != vp9dec.AltrefFrame {
		t.Fatalf("visible inter ref = %v, want ALTREF", got.RefFrame)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned !ok after visible altref-only inter")
	}
	// GOOD speed=3 now follows libvpx select_tx_mode and codes this
	// inter frame as ALLOW_32X32 instead of the old pinned TX_MODE_SELECT
	// path. The reconstructed luma remains visibly tied to the altref
	// refresh; keep a broad gate instead of turning this flag-routing test
	// into a byte oracle.
	if avg := vp9AveragePlaneForTest(t, frame.Y, frame.YStride, width, height); avg < 150 || avg > 205 {
		t.Fatalf("average Y = %d, want altref-like luma in [150,205]", avg)
	}
}

func vp9AveragePlaneForTest(t *testing.T, plane []byte, stride, width, height int) int {
	t.Helper()
	if stride < width {
		t.Fatalf("stride = %d, want at least %d", stride, width)
	}
	var sum int
	for row := range height {
		off := row * stride
		if off+width > len(plane) {
			t.Fatalf("plane too short for row %d width %d stride %d len %d",
				row, width, stride, len(plane))
		}
		for _, v := range plane[off : off+width] {
			sum += int(v)
		}
	}
	return sum / (width * height)
}

func TestVP9EncoderEncodeIntoWithFlagsNoUpdateEntropyRestoresFrameContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewCheckerYCbCr(width, height, 0, 255, 128, 128)
	if _, err := e.Encode(keySrc); err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	before := e.fc
	interSrc := vp9test.NewCheckerYCbCr(width, height, 255, 0, 128, 128)
	dst := make([]byte, 65536)
	if _, err := e.EncodeIntoWithFlags(interSrc, dst, EncodeNoUpdateEntropy); err != nil {
		t.Fatalf("EncodeIntoWithFlags no-update-entropy: %v", err)
	}
	if e.fc != before {
		t.Fatal("frame context changed after EncodeNoUpdateEntropy")
	}
}

func TestVP9EncoderErrorResilientRestoresDefaultFrameContext(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: width, Height: height, ErrorResilient: true,
	})
	src := vp9test.NewCheckerYCbCr(width, height, 0, 255, 128, 128)
	if _, err := e.Encode(src); err != nil {
		t.Fatalf("Encode error-resilient keyframe: %v", err)
	}
	var want vp9dec.FrameContext
	vp9dec.ResetFrameContext(&want)
	if e.fc != want {
		t.Fatal("frame context changed after error-resilient keyframe")
	}
	if _, err := e.Encode(vp9test.NewCheckerYCbCr(width, height, 255, 0, 128, 128)); err != nil {
		t.Fatalf("Encode error-resilient inter: %v", err)
	}
	if e.fc != want {
		t.Fatal("frame context changed after error-resilient inter frame")
	}
}

// TestVP9EncoderEncodeIntoWithFlagsAcceptsNoUpdateOnKeyFrame pins
// libvpx's "NoUpdate hints are silently ignored on KEY_FRAMEs" rule
// from vp9/encoder/vp9_encoder.c:856-858 (KEY_FRAME path forces
// cpi->refresh_golden_frame = 1 and cpi->refresh_alt_ref_frame = 1) and
// vp9_encoder.c:5444 (KEY_FRAME path forces cpi->refresh_last_frame = 1)
// even after set_ext_overrides at vp9_encoder.c:4761-4775 copied the
// user-supplied ext_refresh_*_frame fields. The net effect is that an
// EncodeNoUpdate{Last,Golden,AltRef} flag passed alongside an implicit
// or explicit KEY_FRAME never errors — libvpx encodes the keyframe and
// the NoUpdate hint becomes a no-op. govpx writes
// header.RefreshFrameFlags = 0xff on KEY_FRAMEs (vp9_encoder.go: at the
// isKey branch) unconditionally, mirroring this.

func TestVP9EncoderEncodeIntoWithFlagsAcceptsNoUpdateOnKeyFrame(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	src := vp9test.NewYCbCr(width, height, 96, 128, 128)
	dst := make([]byte, 65536)
	for _, flags := range []EncodeFlags{
		EncodeNoUpdateLast,
		EncodeNoUpdateGolden,
		EncodeNoUpdateAltRef,
		EncodeNoUpdateLast | EncodeNoUpdateGolden | EncodeNoUpdateAltRef,
	} {
		if _, err := e.EncodeIntoWithFlags(src, dst, flags); err != nil {
			t.Fatalf("flags %#x on implicit KEY_FRAME err = %v, want nil (libvpx silently ignores NoUpdate hints on KEY_FRAMEs)", flags, err)
		}
		// Reset so each iteration encodes a fresh KEY_FRAME via frame_index=0.
		e.Close()
		e, _ = NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	}
	e.Close()
}
