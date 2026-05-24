package govpx

import (
	"encoding/binary"
	"image"
	"runtime"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

const (
	vp9EncoderKeyframeAllocRuns = 10
	vp9EncoderInterAllocRuns    = 3
)

func vp9SteadyStateAllocsPerRun(warmRuns int, runs int, f func()) float64 {
	if runs <= 0 {
		return 0
	}
	oldProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldProcs)
	runtime.GC()
	for range warmRuns {
		f()
	}
	var before runtime.MemStats
	var after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return float64(after.Mallocs-before.Mallocs) / float64(runs)
}

func assertVP9InterMotionBlockForTest(t *testing.T, name string,
	mi vp9dec.NeighborMi, want vp9dec.MV,
) {
	t.Helper()
	if mi.Mode != common.NearestMv && mi.Mode != common.NearMv && mi.Mode != common.NewMv {
		t.Fatalf("%s block mode = %d, want an inter motion mode", name, mi.Mode)
	}
	if mi.Mv[0] != want {
		t.Fatalf("%s block MV = %+v, want %+v", name, mi.Mv[0], want)
	}
}

func shiftedVP9ReferenceYCbCrForTest(ref Image, dx, dy int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeDx, planeDy int) {
		for y := range height {
			dstRow := dst[y*dstStride:]
			sy := clampVP9IntForTest(y+planeDy, 0, height-1)
			srcRow := src[sy*srcStride:]
			for x := range width {
				sx := clampVP9IntForTest(x+planeDx, 0, width-1)
				dstRow[x] = srcRow[sx]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, dx, dy)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, dx>>1, dy>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, dx>>1, dy>>1)
	return img
}

func vp9ImageFromYCbCrForTest(img *image.YCbCr) Image {
	return Image{
		Width:   img.Rect.Dx(),
		Height:  img.Rect.Dy(),
		Y:       img.Y,
		U:       img.Cb,
		V:       img.Cr,
		YStride: img.YStride,
		UStride: img.CStride,
		VStride: img.CStride,
	}
}

func decodeVP9PacketMiGridForOracleTest(t *testing.T, packet []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode packet: %v", err)
	}
	out := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(out, d.miGrid)
	return out
}

func decodeVP9TwoFrameInterMiGridForOracleTest(t *testing.T, key, inter []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode key packet: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned !ok after key packet")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter packet: %v", err)
	}
	out := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(out, d.miGrid)
	return out
}

func splitShiftedVP9ReferenceYCbCrForTest(ref Image, leftDx, rightDx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeLeftDx, planeRightDx int) {
		splitX := width / 2
		for y := range height {
			dstRow := dst[y*dstStride:]
			srcRow := src[y*srcStride:]
			for x := range width {
				dx := planeLeftDx
				if x >= splitX {
					dx = planeRightDx
				}
				sx := clampVP9IntForTest(x+dx, 0, width-1)
				dstRow[x] = srcRow[sx]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, leftDx, rightDx)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, leftDx>>1, rightDx>>1)
	return img
}

func splitYShiftedVP9ReferenceYCbCrForTest(ref Image, topDy, bottomDy int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height, planeTopDy, planeBottomDy int) {
		splitY := height / 2
		for y := range height {
			dy := planeTopDy
			if y >= splitY {
				dy = planeBottomDy
			}
			sy := clampVP9IntForTest(y+dy, 0, height-1)
			dstRow := dst[y*dstStride:]
			srcRow := src[sy*srcStride:]
			for x := range width {
				dstRow[x] = srcRow[x]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height, topDy, bottomDy)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight, topDy>>1, bottomDy>>1)
	return img
}

func quadrantShiftedVP9ReferenceYCbCrForTest(ref Image,
	topLeft, topRight, bottomLeft, bottomRight image.Point,
) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	shiftPlane := func(dst []byte, dstStride int, src []byte, srcStride, width, height int,
		tl, tr, bl, br image.Point,
	) {
		splitX := width / 2
		splitY := height / 2
		for y := range height {
			dstRow := dst[y*dstStride:]
			for x := range width {
				shift := tl
				if y >= splitY {
					shift = bl
					if x >= splitX {
						shift = br
					}
				} else if x >= splitX {
					shift = tr
				}
				srcX := clampVP9IntForTest(x+shift.X, 0, width-1)
				srcY := clampVP9IntForTest(y+shift.Y, 0, height-1)
				dstRow[x] = src[srcY*srcStride+srcX]
			}
		}
	}
	shiftPlane(img.Y, img.YStride, ref.Y, ref.YStride, ref.Width, ref.Height,
		topLeft, topRight, bottomLeft, bottomRight)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	uvTopLeft := image.Point{X: topLeft.X >> 1, Y: topLeft.Y >> 1}
	uvTopRight := image.Point{X: topRight.X >> 1, Y: topRight.Y >> 1}
	uvBottomLeft := image.Point{X: bottomLeft.X >> 1, Y: bottomLeft.Y >> 1}
	uvBottomRight := image.Point{X: bottomRight.X >> 1, Y: bottomRight.Y >> 1}
	shiftPlane(img.Cb, img.CStride, ref.U, ref.UStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
	shiftPlane(img.Cr, img.CStride, ref.V, ref.VStride, uvWidth, uvHeight,
		uvTopLeft, uvTopRight, uvBottomLeft, uvBottomRight)
	return img
}

func predictedVP9ReferenceYCbCrForTest(t *testing.T, ref Image, mv vp9dec.MV) *image.YCbCr {
	t.Helper()
	var d VP9Decoder
	vp9dec.SetupBlockPlanes(&d.planes, 1, 1)
	d.prepareVP9OutputFrame(ref.Width, ref.Height)
	d.refFrames[0].store(ref)
	hdr := vp9dec.UncompressedHeader{
		Width:  uint32(ref.Width),
		Height: uint32(ref.Height),
		InterRef: vp9dec.InterRefBlock{
			RefIndex: [3]uint8{0, 0, 0},
		},
		InterpFilter: vp9dec.InterpEighttap,
	}
	miRows := (ref.Height + 7) >> 3
	miCols := (ref.Width + 7) >> 3
	for miRow := 0; miRow < miRows; miRow += common.MiBlockSize {
		for miCol := 0; miCol < miCols; miCol += common.MiBlockSize {
			bsize := vp9StubBlockSizeForRegion(miRows, miCols,
				miRow, miCol, common.Block64x64)
			mi := vp9dec.NeighborMi{
				SbType:       bsize,
				Mode:         common.NewMv,
				InterpFilter: uint8(vp9dec.InterpEighttap),
				RefFrame: [2]int8{
					vp9dec.LastFrame,
					vp9dec.NoRefFrame,
				},
				Mv: [2]vp9dec.MV{mv},
			}
			if !d.reconstructVP9InterPredictBlock(&hdr, &mi,
				miRow, miCol, vp9dec.ModeInfoDecodeBSize(bsize)) {
				t.Fatalf("reconstruct predictor block at mi %d,%d failed", miRow, miCol)
			}
		}
	}
	img := image.NewYCbCr(image.Rect(0, 0, ref.Width, ref.Height), image.YCbCrSubsampleRatio420)
	copyPlane(img.Y, img.YStride, d.lastFrame.Y, d.lastFrame.YStride, ref.Width, ref.Height)
	uvWidth := (ref.Width + 1) >> 1
	uvHeight := (ref.Height + 1) >> 1
	copyPlane(img.Cb, img.CStride, d.lastFrame.U, d.lastFrame.UStride, uvWidth, uvHeight)
	copyPlane(img.Cr, img.CStride, d.lastFrame.V, d.lastFrame.VStride, uvWidth, uvHeight)
	return img
}

func decodeVP9KeyInterForTest(t *testing.T, key, inter []byte) *VP9Decoder {
	t.Helper()
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
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter: %v", err)
	}
	return d
}

func clampVP9IntForTest(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// TestNewVP9EncoderRequiresDimensions: Width and Height must both be
// positive; zero or negative values get rejected with
// ErrInvalidConfig.

func newVP9KeyframeModeTestState(e *VP9Encoder, img *image.YCbCr, width, height int) *vp9KeyframeEncodeState {
	vp9dec.ResetFrameContext(&e.fc)
	hdr := &vp9dec.UncompressedHeader{Width: uint32(width), Height: uint32(height)}
	dq := &vp9dec.DequantTables{}
	var seg vp9dec.SegmentationParams
	vp9dec.SetupSegmentationDequant(&seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: e.vp9EncoderModeDecisionQIndex(),
		BitDepth:   vp9dec.Bits8,
	}, dq)
	return &vp9KeyframeEncodeState{img: img, hdr: hdr, dq: dq}
}

func assertVP9VisibleYContrast(t *testing.T, got Image, width, height int, minDelta byte) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	if got.YStride < width || len(got.Y) < buffers.PlaneLen(got.YStride, height, width) {
		t.Fatalf("Y plane shape = len %d stride %d, want %dx%d",
			len(got.Y), got.YStride, width, height)
	}
	lo, hi := byte(255), byte(0)
	for y := range height {
		row := got.Y[y*got.YStride:]
		for x := range width {
			v := row[x]
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
	}
	if hi-lo < minDelta {
		t.Fatalf("visible Y contrast = %d..%d, want delta >= %d", lo, hi, minDelta)
	}
}

func vp9VisibleImageEqual(a, b Image) bool {
	if a.Width != b.Width || a.Height != b.Height {
		return false
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(a.Width, a.Height)
	return testutil.PlaneEqual(a.Y, a.YStride, b.Y, b.YStride, a.Width, a.Height) &&
		testutil.PlaneEqual(a.U, a.UStride, b.U, b.UStride, uvWidth, uvHeight) &&
		testutil.PlaneEqual(a.V, a.VStride, b.V, b.VStride, uvWidth, uvHeight)
}

func assertVP9ImageMatchesYCbCr(t *testing.T, name string, got Image, want *image.YCbCr) {
	t.Helper()
	wantImage := vp9ImageFromYCbCrForTest(want)
	if got.Width != wantImage.Width || got.Height != wantImage.Height {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d",
			name, got.Width, got.Height, wantImage.Width, wantImage.Height)
	}
	checkPlane := func(label string, gotPlane []byte, gotStride int,
		wantPlane []byte, wantStride, width, height int,
	) {
		t.Helper()
		for y := range height {
			gotRow := gotPlane[y*gotStride:]
			wantRow := wantPlane[y*wantStride:]
			for x := range width {
				if gotRow[x] != wantRow[x] {
					t.Fatalf("%s %s[%d,%d] = %d, want %d",
						name, label, y, x, gotRow[x], wantRow[x])
				}
			}
		}
	}
	checkPlane("Y", got.Y, got.YStride, wantImage.Y, wantImage.YStride,
		wantImage.Width, wantImage.Height)
	uvWidth := (wantImage.Width + 1) >> 1
	uvHeight := (wantImage.Height + 1) >> 1
	checkPlane("U", got.U, got.UStride, wantImage.U, wantImage.UStride,
		uvWidth, uvHeight)
	checkPlane("V", got.V, got.VStride, wantImage.V, wantImage.VStride,
		uvWidth, uvHeight)
}

func assertVP9VisibleChromaContrast(t *testing.T, got Image, width, height int, minDelta byte) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	if got.UStride < uvWidth || got.VStride < uvWidth ||
		len(got.U) < buffers.PlaneLen(got.UStride, uvHeight, uvWidth) ||
		len(got.V) < buffers.PlaneLen(got.VStride, uvHeight, uvWidth) {
		t.Fatalf("UV plane shape = U len %d stride %d, V len %d stride %d, want %dx%d",
			len(got.U), got.UStride, len(got.V), got.VStride, uvWidth, uvHeight)
	}
	lo, hi := byte(255), byte(0)
	for y := range uvHeight {
		uRow := got.U[y*got.UStride:]
		vRow := got.V[y*got.VStride:]
		for x := range uvWidth {
			for _, v := range [...]byte{uRow[x], vRow[x]} {
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
		}
	}
	if hi-lo < minDelta {
		t.Fatalf("visible UV contrast = %d..%d, want delta >= %d", lo, hi, minDelta)
	}
}

func assertVP9StaticSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int, altQ, altLF int16,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData || !seg.AbsDelta {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v absDelta:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData, seg.AbsDelta)
	}
	// libvpx's vp9_choose_segmap_coding_method
	// (vp9/encoder/vp9_segmentation.c:242-316) runs unconditionally
	// from encode_segmentation whenever seg->update_map is set
	// (vp9_bitstream.c:773). It counts the realized mi_grid_visible
	// and writes the chosen tree_probs via calc_segtree_probs
	// (vp9_segmentation.c:104-118), so a fully-static map where every
	// block falls in segID has two valid projections depending on
	// whether the chooser picked temporal (t_unpred all-zero counts,
	// every prob = 128) or spatial (no_pred_segcounts[segID]=N,
	// projection over the realized counts).
	var spatialCounts [vp9dec.MaxSegments]uint32
	spatialCounts[segID] = 1
	var spatialTree [vp9dec.SegTreeProbs]uint8
	vp9CalcSegTreeProbs(spatialCounts, &spatialTree)
	var temporalCounts [vp9dec.MaxSegments]uint32 // all zero -> get_binary_prob(0,0) = 128
	var temporalTree [vp9dec.SegTreeProbs]uint8
	vp9CalcSegTreeProbs(temporalCounts, &temporalTree)
	wantTree := spatialTree
	if seg.TemporalUpdate {
		wantTree = temporalTree
	}
	for i := range vp9dec.SegTreeProbs {
		if seg.TreeProbs[i] != wantTree[i] {
			t.Fatalf("TreeProbs[%d] = %d, want %d (temporal_update=%t, libvpx calc_segtree_probs over realized mi_grid)",
				i, seg.TreeProbs[i], wantTree[i], seg.TemporalUpdate)
		}
	}
	wantMask := uint32((1 << uint(vp9dec.SegLvlAltQ)) |
		(1 << uint(vp9dec.SegLvlAltLf)))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want AltQ|AltLF", segID, got)
	}
	if got := seg.FeatureData[segID][vp9dec.SegLvlAltQ]; got != altQ {
		t.Fatalf("AltQ[%d] = %d, want %d", segID, got, altQ)
	}
	if got := seg.FeatureData[segID][vp9dec.SegLvlAltLf]; got != altLF {
		t.Fatalf("AltLF[%d] = %d, want %d", segID, got, altLF)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9StaticSkipSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	wantMask := uint32(1 << uint(vp9dec.SegLvlSkip))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want Skip", segID, got)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9StaticRefFrameSegmentationHeaderForTest(t *testing.T,
	seg vp9dec.SegmentationParams, segID int, refFrame int8,
) {
	t.Helper()
	if !seg.Enabled || !seg.UpdateMap || !seg.UpdateData {
		t.Fatalf("segmentation flags = enabled:%v updateMap:%v updateData:%v, want all true",
			seg.Enabled, seg.UpdateMap, seg.UpdateData)
	}
	wantMask := uint32(1 << uint(vp9dec.SegLvlRefFrame))
	if got := seg.FeatureMask[segID]; got != wantMask {
		t.Fatalf("FeatureMask[%d] = %#x, want RefFrame", segID, got)
	}
	if got := int8(seg.FeatureData[segID][vp9dec.SegLvlRefFrame]); got != refFrame {
		t.Fatalf("RefFrame[%d] = %d, want %d", segID, got, refFrame)
	}
	for i := range vp9dec.MaxSegments {
		if i == segID {
			continue
		}
		if seg.FeatureMask[i] != 0 {
			t.Fatalf("FeatureMask[%d] = %#x, want 0", i, seg.FeatureMask[i])
		}
	}
}

func assertVP9DecoderSegmentIDForTest(t *testing.T, d *VP9Decoder, segID uint8) {
	t.Helper()
	if len(d.miGrid) == 0 {
		t.Fatal("decoder MI grid is empty")
	}
	// On intra frames libvpx's ReadIntraFrameModeInfo stores
	// segment_id in mi.seg_id_predicted (intra_driver.go:117). On
	// inter frames with temporal_update=1 the libvpx decoder writes
	// the predicted-bit (0/1) into seg_id_predicted instead
	// (segment_driver.go:114, mirroring read_inter_segment_id in
	// vp9_decodeframe.c). Now that the chooser runs unconditionally
	// (mirroring vp9_bitstream.c:773), libvpx will pick
	// temporal_update on inter frames whose predicted-bit cost
	// undercuts spatial coding — accept either projection rather
	// than insisting on the legacy intra-only "seg_id_predicted ==
	// segID" shape.
	for i, mi := range d.miGrid {
		if mi.SegmentID != segID {
			t.Fatalf("miGrid[%d] SegmentID = %d, want %d",
				i, mi.SegmentID, segID)
		}
		if mi.SegIDPredicted != segID && mi.SegIDPredicted > 1 {
			t.Fatalf("miGrid[%d] SegIDPredicted = %d, want %d (intra) or 0/1 (inter temporal_update)",
				i, mi.SegIDPredicted, segID)
		}
	}
	if len(d.lastSegMap) == 0 {
		t.Fatal("decoder last segment map is empty")
	}
	for i, got := range d.lastSegMap {
		if got != segID {
			t.Fatalf("lastSegMap[%d] = %d, want %d", i, got, segID)
		}
	}
}

func assertVP9EncoderTilePrefixForTest(t *testing.T, packet []byte, tileStart int) {
	t.Helper()
	if len(packet)-tileStart < 5 {
		t.Fatalf("multi-tile payload too small: tileStart=%d packet=%d",
			tileStart, len(packet))
	}
	firstTileSize := int(binary.BigEndian.Uint32(packet[tileStart : tileStart+4]))
	if firstTileSize <= 0 {
		t.Fatalf("first tile size prefix = %d, want > 0", firstTileSize)
	}
	if tileStart+4+firstTileSize >= len(packet) {
		t.Fatalf("first tile consumes packet: start=%d size=%d len=%d",
			tileStart, firstTileSize, len(packet))
	}
}
