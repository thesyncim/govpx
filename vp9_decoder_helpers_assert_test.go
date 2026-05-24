package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func assertVP9NeutralFrame(t *testing.T, got Image, width, height int) {
	t.Helper()
	assertVP9FilledFrame(t, got, width, height, 128, 128, 128)
}

func vp9DecodeLastVisibleFrameForTest(t *testing.T, packets ...[]byte) Image {
	t.Helper()
	return vp9DecodeLastVisibleFrameWithOptionsForTest(t, VP9DecoderOptions{},
		packets...)
}

func vp9DecodeLastVisibleFrameWithOptionsForTest(t *testing.T,
	opts VP9DecoderOptions, packets ...[]byte,
) Image {
	t.Helper()
	d, err := NewVP9Decoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	var last Image
	ok := false
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		if frame, frameOK := d.NextFrame(); frameOK {
			last = frame
			ok = true
		}
	}
	if !ok {
		t.Fatal("packet sequence did not publish a visible frame")
	}
	return last
}

func assertVP9ImagesEqual(t *testing.T, want, got Image) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, want.Width, want.Height)
	}
	if !vp9VisiblePlanesEqual(want.Y, want.YStride, got.Y, got.YStride,
		want.Width, want.Height) {
		t.Fatal("Y plane differs")
	}
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	if !vp9VisiblePlanesEqual(want.U, want.UStride, got.U, got.UStride,
		uvWidth, uvHeight) {
		t.Fatal("U plane differs")
	}
	if !vp9VisiblePlanesEqual(want.V, want.VStride, got.V, got.VStride,
		uvWidth, uvHeight) {
		t.Fatal("V plane differs")
	}
}

func vp9VisiblePlanesEqual(a []byte, aStride int, b []byte, bStride int,
	width, height int,
) bool {
	for row := range height {
		aStart := row * aStride
		bStart := row * bStride
		if !bytes.Equal(a[aStart:aStart+width], b[bStart:bStart+width]) {
			return false
		}
	}
	return true
}

func appendVP9YForTest(out []byte, img Image) []byte {
	return testutil.AppendPlane(out, img.Y, img.YStride, img.Width, img.Height)
}

func vp9YRectDiffers(a, b Image, x, y, width, height int) bool {
	for row := y; row < y+height; row++ {
		for col := x; col < x+width; col++ {
			if a.Y[row*a.YStride+col] != b.Y[row*b.YStride+col] {
				return true
			}
		}
	}
	return false
}

func fillVP9PublicImage(img *Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func assertVP9FilledFrame(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	assertVP9PlaneFilled(t, "Y", got.Y, got.YStride, width, height, yValue)
	assertVP9PlaneFilled(t, "U", got.U, got.UStride, uvWidth, uvHeight, uValue)
	assertVP9PlaneFilled(t, "V", got.V, got.VStride, uvWidth, uvHeight, vValue)
}

func assertVP9FilledFrameWithin(t *testing.T, got Image, width, height int,
	yValue, uValue, vValue, tolerance byte,
) {
	t.Helper()
	if got.Width != width || got.Height != height {
		t.Fatalf("frame dimensions = %dx%d, want %dx%d",
			got.Width, got.Height, width, height)
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	assertVP9PlaneFilledWithin(t, "Y", got.Y, got.YStride, width, height,
		yValue, tolerance)
	assertVP9PlaneFilledWithin(t, "U", got.U, got.UStride, uvWidth, uvHeight,
		uValue, tolerance)
	assertVP9PlaneFilledWithin(t, "V", got.V, got.VStride, uvWidth, uvHeight,
		vValue, tolerance)
}

func assertVP9PlaneFilled(t *testing.T, name string, plane []byte,
	stride, width, height int, want byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	wantLen := buffers.PlaneLen(stride, height, width)
	if len(plane) < wantLen {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), wantLen)
	}
	for row := range height {
		for col := range width {
			if got := plane[row*stride+col]; got != want {
				t.Fatalf("%s[%d,%d] = %d, want %d",
					name, row, col, got, want)
			}
		}
	}
}

func assertVP9PlaneFilledWithin(t *testing.T, name string, plane []byte,
	stride, width, height int, want, tolerance byte,
) {
	t.Helper()
	if stride < width {
		t.Fatalf("%s stride = %d, want at least %d", name, stride, width)
	}
	wantLen := buffers.PlaneLen(stride, height, width)
	if len(plane) < wantLen {
		t.Fatalf("%s plane len = %d, want at least %d",
			name, len(plane), wantLen)
	}
	for row := range height {
		for col := range width {
			got := plane[row*stride+col]
			if vp9AbsInt(int(got)-int(want)) > int(tolerance) {
				t.Fatalf("%s[%d,%d] = %d, want %d +/- %d",
					name, row, col, got, want, tolerance)
			}
		}
	}
}

func vp9TileStartForTest(packet []byte) (int, error) {
	var br vp9dec.BitReader
	br.Init(packet)
	hdr, err := vp9dec.ReadUncompressedHeader(&br, nil, nil)
	if err != nil {
		return 0, err
	}
	return br.BytesRead() + int(hdr.FirstPartitionSize), nil
}

func vp9MultiTileStubPacketForTest(t *testing.T, width, height, log2TileCols int) []byte {
	t.Helper()
	return vp9StubPacketForTest(t, width, height, log2TileCols, common.DcPred)
}

func vp9MultiTileStubPacketWithFrameParallelForTest(t *testing.T,
	width, height, log2TileCols int, frameParallel bool,
) []byte {
	t.Helper()
	return vp9StubPacketWithFrameParallelForTest(t, width, height,
		log2TileCols, common.DcPred, frameParallel)
}

func vp9StubPacketForTest(t *testing.T, width, height, log2TileCols int,
	yMode common.PredictionMode,
) []byte {
	t.Helper()
	return vp9StubPacketWithFrameParallelForTest(t, width, height,
		log2TileCols, yMode, true)
}

func vp9StubPacketWithFrameParallelForTest(t *testing.T, width, height,
	log2TileCols int, yMode common.PredictionMode, frameParallel bool,
) []byte {
	t.Helper()
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)
	vp9dec.ResetFrameContext(&e.fc)
	e.aboveSegCtx = make([]int8, common.AlignToSB(miCols))
	e.leftSegCtx = make([]int8, common.MiBlockSize)
	e.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: frameParallel,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols
	header.Tile.Log2TileRows = 0

	baseMi := vp9dec.NeighborMi{
		SbType: common.Block64x64,
		Mode:   yMode,
		TxSize: common.Tx4x4,
		Skip:   1,
		RefFrame: [2]int8{
			vp9dec.IntraFrame,
			vp9dec.NoRefFrame,
		},
	}
	var seg vp9dec.SegmentationParams
	partitionProbs := tables.KfPartitionProbs
	tileCols := 1 << uint(log2TileCols)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: tileCols,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9dec.TileOffset(tileRow, miRows, header.Tile.Log2TileRows),
				MiRowEnd:   vp9dec.TileOffset(tileRow+1, miRows, header.Tile.Log2TileRows),
				MiColStart: vp9dec.TileOffset(tileCol, miCols, header.Tile.Log2TileCols),
				MiColEnd:   vp9dec.TileOffset(tileCol+1, miCols, header.Tile.Log2TileCols),
			}
			e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
				&partitionProbs, &seg, baseMi, txModeForMi(baseMi),
				vp9ModeTreeKeyframe, nil, nil)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}

func vp9MultiTileModePacketForTest(t *testing.T, width, height,
	log2TileCols int, modes []common.PredictionMode,
) []byte {
	t.Helper()
	if len(modes) == 0 {
		t.Fatal("vp9MultiTileModePacketForTest requires at least one mode")
	}
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	w := uint32(width)
	h := uint32(height)
	miCols := int((w + 7) >> 3)
	miRows := int((h + 7) >> 3)
	vp9dec.ResetFrameContext(&e.fc)
	e.aboveSegCtx = make([]int8, common.AlignToSB(miCols))
	e.leftSegCtx = make([]int8, common.MiBlockSize)
	e.miGrid = make([]vp9dec.NeighborMi, miRows*miCols)

	header := vp9dec.UncompressedHeader{
		Profile:               common.Profile0,
		FrameType:             common.KeyFrame,
		ShowFrame:             true,
		RefreshFrameFlags:     0xff,
		Width:                 w,
		Height:                h,
		RefreshFrameContext:   true,
		FrameParallelDecoding: true,
		InterpFilter:          vp9dec.InterpEighttap,
		BitDepthColor: vp9dec.BitdepthColorspaceSampling{
			BitDepth:   vp9dec.Bits8,
			ColorSpace: common.CSUnknown,
			ColorRange: common.CRStudioRange,
		},
	}
	header.Quant.BaseQindex = 1
	header.Tile.Log2TileCols = log2TileCols

	var seg vp9dec.SegmentationParams
	partitionProbs := tables.KfPartitionProbs
	tileCols := 1 << uint(log2TileCols)
	dest := make([]byte, 262144)
	scratch := make([]byte, 262144)
	n, err := vp9enc.PackBitstream(vp9enc.PackBitstreamArgs{
		Dest:    dest,
		Scratch: scratch,
		Header:  &header,
		Comp: vp9enc.CompressedHeaderInputs{
			Lossless:           false,
			TxMode:             common.Only4x4,
			IntraOnly:          true,
			InterpFilter:       vp9dec.InterpEighttap,
			ReferenceMode:      vp9dec.SingleReference,
			CompoundRefAllowed: false,
		},
		TileRows: 1,
		TileCols: tileCols,
		WriteTile: func(bw *bitstream.Writer, tileRow, tileCol int) error {
			tile := vp9dec.TileBounds{
				MiRowStart: vp9dec.TileOffset(tileRow, miRows, header.Tile.Log2TileRows),
				MiRowEnd:   vp9dec.TileOffset(tileRow+1, miRows, header.Tile.Log2TileRows),
				MiColStart: vp9dec.TileOffset(tileCol, miCols, header.Tile.Log2TileCols),
				MiColEnd:   vp9dec.TileOffset(tileCol+1, miCols, header.Tile.Log2TileCols),
			}
			baseMi := vp9dec.NeighborMi{
				SbType: common.Block64x64,
				Mode:   modes[tileCol%len(modes)],
				TxSize: common.Tx4x4,
				Skip:   1,
				RefFrame: [2]int8{
					vp9dec.IntraFrame,
					vp9dec.NoRefFrame,
				},
			}
			e.writeVP9ModesTileBounds(bw, miRows, miCols, tile,
				&partitionProbs, &seg, baseMi, txModeForMi(baseMi),
				vp9ModeTreeKeyframe, nil, nil)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("PackBitstream: %v", err)
	}
	packet := make([]byte, n)
	copy(packet, dest[:n])
	return packet
}
