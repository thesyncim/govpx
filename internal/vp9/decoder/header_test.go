package decoder

import (
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"testing"
)

func TestHeaderRenderSize(t *testing.T) {
	hdr := &UncompressedHeader{Width: 640, Height: 360}
	if w, h := HeaderRenderSize(hdr); w != 640 || h != 360 {
		t.Fatalf("fallback render size = %dx%d, want 640x360", w, h)
	}
	hdr.Render.Width = 320
	hdr.Render.Height = 180
	if w, h := HeaderRenderSize(hdr); w != 320 || h != 180 {
		t.Fatalf("explicit render size = %dx%d, want 320x180", w, h)
	}
	if w, h := HeaderRenderSize(nil); w != 0 || h != 0 {
		t.Fatalf("nil render size = %dx%d, want 0x0", w, h)
	}
}

func TestSupportedOutputFormat(t *testing.T) {
	hdr := &UncompressedHeader{}
	hdr.Profile = common.Profile0
	hdr.BitDepthColor.BitDepth = Bits8
	hdr.BitDepthColor.SubsamplingX = 1
	hdr.BitDepthColor.SubsamplingY = 1
	if !SupportedOutputFormat(hdr) {
		t.Fatal("profile0 8-bit 4:2:0 header rejected")
	}
	hdr.Profile = common.Profile1
	if SupportedOutputFormat(hdr) {
		t.Fatal("profile1 header accepted")
	}
	hdr.Profile = common.Profile0
	hdr.BitDepthColor.BitDepth = Bits10
	if SupportedOutputFormat(hdr) {
		t.Fatal("10-bit header accepted")
	}
}

func TestFrameRefSignBiasAndCompoundAllowedForHeader(t *testing.T) {
	hdr := &UncompressedHeader{FrameType: common.InterFrame}
	hdr.InterRef.SignBias = [common.RefsPerFrame]uint8{0, 1, 0}
	got := FrameRefSignBias(hdr)
	if got[LastFrame] != 0 || got[GoldenFrame] != 1 || got[AltrefFrame] != 0 {
		t.Fatalf("sign bias = %v, want last=0 golden=1 altref=0", got)
	}
	if !CompoundReferenceAllowedForHeader(hdr) {
		t.Fatal("inter header with mixed sign bias rejected for compound refs")
	}
	hdr.FrameType = common.KeyFrame
	if CompoundReferenceAllowedForHeader(hdr) {
		t.Fatal("key frame accepted for compound refs")
	}
}

func TestHeaderResetsPastIndependence(t *testing.T) {
	for _, hdr := range []*UncompressedHeader{
		{FrameType: common.KeyFrame},
		{IntraOnly: true},
		{ErrorResilientMode: true},
	} {
		if !HeaderResetsPastIndependence(hdr) {
			t.Fatalf("HeaderResetsPastIndependence(%+v) = false, want true", hdr)
		}
	}
	if HeaderResetsPastIndependence(&UncompressedHeader{FrameType: common.InterFrame}) {
		t.Fatal("plain inter header reset past independence")
	}
}

func TestPartitionContextUpdateWidth(t *testing.T) {
	for _, tc := range []struct {
		half int
		want int
	}{
		{0, 1},
		{1, 2},
		{2, 4},
		{4, 8},
	} {
		if got := PartitionContextUpdateWidth(tc.half); got != tc.want {
			t.Fatalf("PartitionContextUpdateWidth(%d) = %d, want %d",
				tc.half, got, tc.want)
		}
	}
}

func TestPlaneEntropyLenAndTileOffset(t *testing.T) {
	if got := PlaneEntropyLen(8, 0); got != 16 {
		t.Fatalf("PlaneEntropyLen luma = %d, want 16", got)
	}
	if got := PlaneEntropyLen(8, 1); got != 8 {
		t.Fatalf("PlaneEntropyLen chroma = %d, want 8", got)
	}
	if got := TileOffset(1, 17, 1); got != 8 {
		t.Fatalf("TileOffset(1,17,1) = %d, want 8", got)
	}
	if got := TileOffset(2, 17, 1); got != 17 {
		t.Fatalf("TileOffset(2,17,1) = %d, want 17", got)
	}
}

func TestReadLoopfilterNoDeltas(t *testing.T) {
	// filter_level=42 (6 bits), sharpness_level=3 (3 bits),
	// mode_ref_delta_enabled=0.
	var pk bitPacker
	pk.writeLiteral(42, 6)
	pk.writeLiteral(3, 3)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var lf LoopfilterParams
	ReadLoopfilter(&r, &lf)
	if lf.FilterLevel != 42 {
		t.Errorf("FilterLevel = %d, want 42", lf.FilterLevel)
	}
	if lf.SharpnessLevel != 3 {
		t.Errorf("SharpnessLevel = %d, want 3", lf.SharpnessLevel)
	}
	if lf.ModeRefDeltaEnabled {
		t.Error("ModeRefDeltaEnabled should be false")
	}
}

func TestReadLoopfilterWithDeltas(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(20, 6) // filter_level
	pk.writeLiteral(1, 3)  // sharpness_level
	pk.writeBit(1)         // mode_ref_delta_enabled
	pk.writeBit(1)         // mode_ref_delta_update
	// Update ref deltas: only slot 1 with value -5.
	pk.writeBit(0)
	pk.writeBit(1) // slot 1 enable
	pk.writeLiteral(5, 6)
	pk.writeBit(1) // sign = negative
	pk.writeBit(0)
	pk.writeBit(0)
	// Update mode deltas: slot 0 with value 7, slot 1 disabled.
	pk.writeBit(1)
	pk.writeLiteral(7, 6)
	pk.writeBit(0) // sign = positive
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var lf LoopfilterParams
	ReadLoopfilter(&r, &lf)
	if lf.FilterLevel != 20 {
		t.Errorf("FilterLevel = %d, want 20", lf.FilterLevel)
	}
	if !lf.ModeRefDeltaEnabled || !lf.ModeRefDeltaUpdate {
		t.Errorf("delta flags wrong: enabled=%v update=%v", lf.ModeRefDeltaEnabled, lf.ModeRefDeltaUpdate)
	}
	if lf.RefDeltas[1] != -5 {
		t.Errorf("RefDeltas[1] = %d, want -5", lf.RefDeltas[1])
	}
	if lf.ModeDeltas[0] != 7 {
		t.Errorf("ModeDeltas[0] = %d, want 7", lf.ModeDeltas[0])
	}
}

func TestReadQuantizationLossless(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(0, 8) // base_qindex = 0
	pk.writeBit(0)        // y_dc skip
	pk.writeBit(0)        // uv_dc skip
	pk.writeBit(0)        // uv_ac skip
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var q QuantizationParams
	ReadQuantization(&r, &q)
	if !q.Lossless {
		t.Errorf("expected Lossless=true, got Q=%+v", q)
	}
}

func TestReadQuantizationLossy(t *testing.T) {
	var pk bitPacker
	pk.writeLiteral(128, 8) // base_qindex = 128
	pk.writeBit(1)          // y_dc delta present
	pk.writeLiteral(3, 4)   // magnitude
	pk.writeBit(0)          // positive sign
	pk.writeBit(0)          // uv_dc skip
	pk.writeBit(0)          // uv_ac skip
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var q QuantizationParams
	ReadQuantization(&r, &q)
	if q.BaseQindex != 128 {
		t.Errorf("BaseQindex = %d, want 128", q.BaseQindex)
	}
	if q.YDcDeltaQ != 3 {
		t.Errorf("YDcDeltaQ = %d, want 3", q.YDcDeltaQ)
	}
	if q.Lossless {
		t.Error("expected Lossless=false")
	}
}

// TestTileNBits exercises tileNBits boundary cases against libvpx's
// MAX_TILE_WIDTH_B64=64 and MIN_TILE_WIDTH_B64=4 constants.
func TestTileNBits(t *testing.T) {
	cases := []struct{ miCols, minLog, maxLog int }{
		// 1920x1080 has mi_cols ≈ 240 -> sb64_cols ≈ 30 -> min=0 max=2
		{240, 0, 2},
		// 640x480 -> mi_cols=80 -> sb64_cols=10 -> min=0 max=1
		{80, 0, 1},
		// Single SB column -> min=0 max=0
		{8, 0, 0},
	}
	for _, c := range cases {
		minLog, maxLog := TileNBits(c.miCols)
		if minLog != c.minLog || maxLog != c.maxLog {
			t.Errorf("miCols=%d: got (min=%d, max=%d), want (min=%d, max=%d)",
				c.miCols, minLog, maxLog, c.minLog, c.maxLog)
		}
	}
}

func TestReadTileInfo(t *testing.T) {
	// 1920x1080 -> miCols=240. Encode log2_tile_cols=2 (two trailing 1
	// bits, then 0), log2_tile_rows=2 (two 1 bits).
	var pk bitPacker
	pk.writeBit(1) // expand toward max
	pk.writeBit(1)
	pk.writeBit(1) // log2_tile_rows first bit
	pk.writeBit(1) // log2_tile_rows second bit
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var ti TileInfo
	if err := ReadTileInfo(&r, 240, &ti); err != nil {
		t.Fatalf("ReadTileInfo: %v", err)
	}
	if ti.Log2TileCols != 2 {
		t.Errorf("Log2TileCols = %d, want 2", ti.Log2TileCols)
	}
	if ti.Log2TileRows != 2 {
		t.Errorf("Log2TileRows = %d, want 2", ti.Log2TileRows)
	}
}

func TestReadSegmentationDisabled(t *testing.T) {
	var pk bitPacker
	pk.writeBit(0) // enabled = false
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	ReadSegmentation(&r, &seg)
	if seg.Enabled || seg.UpdateMap || seg.UpdateData {
		t.Errorf("expected all flags false, got %+v", seg)
	}
}

func TestReadSegmentationEnabledNoUpdates(t *testing.T) {
	// enabled=1, update_map=0, update_data=0.
	var pk bitPacker
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	// Seed previous-frame state to verify it is preserved.
	seg.TreeProbs[0] = 99
	seg.FeatureData[1][SegLvlAltQ] = 17
	ReadSegmentation(&r, &seg)
	if !seg.Enabled {
		t.Error("Enabled should be true")
	}
	if seg.UpdateMap || seg.UpdateData {
		t.Error("UpdateMap / UpdateData should be false")
	}
	if seg.TreeProbs[0] != 99 {
		t.Errorf("TreeProbs[0] = %d, want 99 (preserved)", seg.TreeProbs[0])
	}
	if seg.FeatureData[1][SegLvlAltQ] != 17 {
		t.Errorf("FeatureData[1][AltQ] = %d, want 17 (preserved)", seg.FeatureData[1][SegLvlAltQ])
	}
}

func TestReadSegmentationFullUpdate(t *testing.T) {
	// enabled=1, update_map=1, all 7 tree probs default to MAX_PROB,
	// temporal_update=0 (so PredProbs <- MAX_PROB), update_data=1,
	// abs_delta=0, segment 0 enables SEG_LVL_ALT_Q with value -10
	// and SEG_LVL_REF_FRAME with value 2, all other features off.
	var pk bitPacker
	pk.writeBit(1) // enabled
	pk.writeBit(1) // update_map
	for range SegTreeProbs {
		pk.writeBit(0) // each prob defaults
	}
	pk.writeBit(0) // temporal_update=0
	pk.writeBit(1) // update_data
	pk.writeBit(0) // abs_delta=0
	// segment 0: ALT_Q enabled, magnitude 10, sign negative
	pk.writeBit(1) // feature 0 enabled (ALT_Q, signed, max=255)
	pk.writeLiteral(10, 8)
	pk.writeBit(1) // sign = negative
	pk.writeBit(0) // feature 1 disabled (ALT_LF)
	pk.writeBit(1) // feature 2 enabled (REF_FRAME, unsigned, max=3, 2 bits)
	pk.writeLiteral(2, 2)
	pk.writeBit(0) // feature 3 disabled (SKIP, no data)
	// segments 1..7: all features disabled (28 bits).
	for i := 1; i < MaxSegments; i++ {
		for range SegLvlMax {
			pk.writeBit(0)
		}
	}
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	var seg SegmentationParams
	ReadSegmentation(&r, &seg)
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Errorf("flags wrong: %+v", seg)
	}
	for i := range SegTreeProbs {
		if seg.TreeProbs[i] != MaxProb {
			t.Errorf("TreeProbs[%d] = %d, want MaxProb", i, seg.TreeProbs[i])
		}
	}
	for i := range PredictionProbs {
		if seg.PredProbs[i] != MaxProb {
			t.Errorf("PredProbs[%d] = %d, want MaxProb", i, seg.PredProbs[i])
		}
	}
	if seg.FeatureMask[0] != (1<<SegLvlAltQ)|(1<<SegLvlRefFrame) {
		t.Errorf("FeatureMask[0] = %b, want %b", seg.FeatureMask[0], (1<<SegLvlAltQ)|(1<<SegLvlRefFrame))
	}
	if seg.FeatureData[0][SegLvlAltQ] != -10 {
		t.Errorf("AltQ = %d, want -10", seg.FeatureData[0][SegLvlAltQ])
	}
	if seg.FeatureData[0][SegLvlRefFrame] != 2 {
		t.Errorf("RefFrame = %d, want 2", seg.FeatureData[0][SegLvlRefFrame])
	}
}

func TestGetUnsignedBits(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 2}, {4, 3}, {63, 6}, {64, 7}, {255, 8}, {256, 9},
	}
	for _, c := range cases {
		if got := getUnsignedBits(c.n); got != c.want {
			t.Errorf("getUnsignedBits(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestReadInterpFilter(t *testing.T) {
	cases := []struct {
		name string
		bits []uint32
		want InterpFilter
	}{
		{"switchable", []uint32{1}, InterpSwitchable},
		{"eighttap_smooth", []uint32{0, 0, 0}, InterpEighttapSmooth},
		{"eighttap", []uint32{0, 0, 1}, InterpEighttap},
		{"eighttap_sharp", []uint32{0, 1, 0}, InterpEighttapSharp},
		{"bilinear", []uint32{0, 1, 1}, InterpBilinear},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pk bitPacker
			for _, b := range c.bits {
				pk.writeBit(b)
			}
			for pk.bitPos&7 != 0 {
				pk.writeBit(0)
			}
			var r BitReader
			r.Init(pk.buf)
			if got := ReadInterpFilter(&r); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestReadRenderSizeInherit(t *testing.T) {
	// flag = 0 → render = (codedWidth, codedHeight)
	var pk bitPacker
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadRenderSize(&r, 1280, 720)
	if got.Width != 1280 || got.Height != 720 {
		t.Errorf("inherit: got %+v", got)
	}
}

func TestReadRenderSizeExplicit(t *testing.T) {
	// flag = 1 then (640-1, 360-1) as two 16-bit literals.
	var pk bitPacker
	pk.writeBit(1)
	pk.writeLiteral(639, 16)
	pk.writeLiteral(359, 16)
	var r BitReader
	r.Init(pk.buf)
	got := ReadRenderSize(&r, 1280, 720)
	if got.Width != 640 || got.Height != 360 {
		t.Errorf("explicit: got %+v", got)
	}
}

func TestReadFrameSizeWithRefsInheritFromGolden(t *testing.T) {
	// last_flag=0, golden_flag=1, altref_flag=0 — Golden's size wins.
	// render_flag=0 — render = (golden's w, h).
	var pk bitPacker
	pk.writeBit(0)
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeBit(0) // render flag inherit
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadFrameSizeWithRefs(&r,
		[3]uint32{1920, 1280, 960},
		[3]uint32{1080, 720, 540},
	)
	if !got.Found || got.FromRef != 1 || got.Width != 1280 || got.Height != 720 {
		t.Errorf("expected inherit-from-1 to (1280,720); got %+v", got)
	}
	if got.Render.Width != 1280 || got.Render.Height != 720 {
		t.Errorf("render = %+v, want (1280,720)", got.Render)
	}
}

func TestReadFrameSizeWithRefsExplicit(t *testing.T) {
	// All three flags = 0; then explicit 1024x768; render flag = 0.
	var pk bitPacker
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeLiteral(1023, 16)
	pk.writeLiteral(767, 16)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadFrameSizeWithRefs(&r,
		[3]uint32{1920, 1280, 960},
		[3]uint32{1080, 720, 540},
	)
	if got.Found {
		t.Errorf("expected !Found, got %+v", got)
	}
	if got.Width != 1024 || got.Height != 768 {
		t.Errorf("expected (1024,768), got (%d,%d)", got.Width, got.Height)
	}
}

func TestReadInterRefBlock(t *testing.T) {
	// Three refs: (idx=5, bias=0), (idx=2, bias=1), (idx=7, bias=0).
	var pk bitPacker
	pk.writeLiteral(5, 3)
	pk.writeBit(0)
	pk.writeLiteral(2, 3)
	pk.writeBit(1)
	pk.writeLiteral(7, 3)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadInterRefBlock(&r)
	wantIdx := [3]uint8{5, 2, 7}
	wantBias := [3]uint8{0, 1, 0}
	if got.RefIndex != wantIdx || got.SignBias != wantBias {
		t.Errorf("got %+v, want RefIndex=%v SignBias=%v", got, wantIdx, wantBias)
	}
}

// buildHeader uses the bitstream writer just for its bit-packing
// utility — strictly speaking the uncompressed header is byte-aligned
// MSB-first rather than the boolean range coding the entropy writer
// uses. We use a small bit-pack helper instead.
type bitPacker struct {
	buf    []byte
	bitPos int
}

func (p *bitPacker) writeBit(bit uint32) {
	if p.bitPos>>3 >= len(p.buf) {
		p.buf = append(p.buf, 0)
	}
	if bit != 0 {
		p.buf[p.bitPos>>3] |= 1 << uint(7-(p.bitPos&7))
	}
	p.bitPos++
}

func (p *bitPacker) writeLiteral(v uint32, bits int) {
	for b := bits - 1; b >= 0; b-- {
		p.writeBit((v >> uint(b)) & 1)
	}
}

// TestReadProfileRoundTrip verifies that ReadProfile correctly decodes
// every legal profile (0..3) when written with the libvpx encoding.
func TestReadProfileRoundTrip(t *testing.T) {
	for p := range uint32(4) {
		var pk bitPacker
		// libvpx's vp9_read_profile decoding:
		//   raw  = bit0 | (bit1 << 1)
		//   if raw > 2 (i.e. raw == 3): raw += bit2
		// So for profile = 3 we write bit0=1, bit1=1, bit2=0 to keep
		// raw at 3 (bit2=1 would push it to invalid profile 4).
		pk.writeBit(p & 1)
		pk.writeBit((p >> 1) & 1)
		if p > 2 {
			pk.writeBit(0)
		}
		// pad to byte boundary
		for pk.bitPos&7 != 0 {
			pk.writeBit(0)
		}

		var r BitReader
		r.Init(pk.buf)
		got := ReadProfile(&r)
		if uint32(got) != p {
			t.Errorf("profile %d: ReadProfile returned %d", p, got)
		}
	}
}

func TestReadFrameMarker(t *testing.T) {
	// Marker is 0b10 = 0x80 in MSB-first 2-bit literal.
	var r BitReader
	r.Init([]byte{0x80})
	if err := ReadFrameMarker(&r); err != nil {
		t.Fatalf("good marker: %v", err)
	}

	r.Init([]byte{0x00}) // 0b00 — bad marker
	if err := ReadFrameMarker(&r); err == nil {
		t.Fatal("bad marker: expected error, got nil")
	}
}

func TestReadSyncCode(t *testing.T) {
	var r BitReader
	r.Init([]byte{common.VP9SyncCode0, common.VP9SyncCode1, common.VP9SyncCode2})
	if !ReadSyncCode(&r) {
		t.Fatal("good sync code not accepted")
	}
	r.Init([]byte{0x00, 0x00, 0x00})
	if ReadSyncCode(&r) {
		t.Fatal("bad sync code accepted")
	}
}

func TestReadFrameSize(t *testing.T) {
	// Encode width=1280, height=720. Wire form is (w-1, h-1) as two
	// 16-bit big-endian literals.
	w, h := uint32(1280), uint32(720)
	var pk bitPacker
	pk.writeLiteral(w-1, 16)
	pk.writeLiteral(h-1, 16)

	var r BitReader
	r.Init(pk.buf)
	gw, gh := ReadFrameSize(&r)
	if gw != w || gh != h {
		t.Errorf("got (%d, %d), want (%d, %d)", gw, gh, w, h)
	}
}

func TestReadBitdepthColorspaceSamplingProfile0(t *testing.T) {
	// Profile 0 / BT.601 / studio range / default 4:2:0 sampling.
	var pk bitPacker
	pk.writeLiteral(uint32(common.CSBT601), 3) // color space
	pk.writeBit(uint32(common.CRStudioRange))  // color range bit
	// Profile 0 doesn't read subsampling bits.
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	got, err := ReadBitdepthColorspaceSampling(&r, common.Profile0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BitDepth != Bits8 {
		t.Errorf("BitDepth = %d, want %d", got.BitDepth, Bits8)
	}
	if got.ColorSpace != common.CSBT601 {
		t.Errorf("ColorSpace = %d, want %d", got.ColorSpace, common.CSBT601)
	}
	if got.ColorRange != common.CRStudioRange {
		t.Errorf("ColorRange = %d, want %d", got.ColorRange, common.CRStudioRange)
	}
	if got.SubsamplingX != 1 || got.SubsamplingY != 1 {
		t.Errorf("Subsampling = (%d, %d), want (1, 1)", got.SubsamplingX, got.SubsamplingY)
	}
}

func TestReadBitdepthColorspaceSamplingProfile0SRGBRejected(t *testing.T) {
	// Profile 0 with SRGB is illegal (4:4:4 only allowed in profile 1/3).
	var pk bitPacker
	pk.writeLiteral(uint32(common.CSSRGB), 3)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	if _, err := ReadBitdepthColorspaceSampling(&r, common.Profile0); err == nil {
		t.Fatal("expected error for SRGB + profile 0")
	}
}

// Unused — silences "bitstream imported but not used" if the package
// already pulls it in elsewhere.
var _ = bitstream.Reader{}
