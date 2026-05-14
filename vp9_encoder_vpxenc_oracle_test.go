package govpx

import (
	"bytes"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

func TestVP9EncoderVpxencOracleKeyframeUncompressedHeaderParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)

	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}

	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	got, _ := parseVP9EncoderHeaderForTest(t, govpxPacket)
	want, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
	assertVP9KeyframeHeaderParity(t, got, want)
}

func assertVP9KeyframeHeaderParity(t *testing.T, got, want vp9dec.UncompressedHeader) {
	t.Helper()
	if got.Profile != want.Profile ||
		got.FrameType != want.FrameType ||
		got.ShowFrame != want.ShowFrame ||
		got.ErrorResilientMode != want.ErrorResilientMode ||
		got.Width != want.Width ||
		got.Height != want.Height ||
		got.Render != want.Render ||
		got.RefreshFrameFlags != want.RefreshFrameFlags ||
		got.RefreshFrameContext != want.RefreshFrameContext ||
		got.FrameParallelDecoding != want.FrameParallelDecoding ||
		got.FrameContextIdx != want.FrameContextIdx ||
		got.InterpFilter != want.InterpFilter ||
		got.Tile != want.Tile ||
		got.Quant != want.Quant ||
		got.Loopfilter != want.Loopfilter ||
		got.Seg != want.Seg {
		t.Fatalf("govpx keyframe header = %+v\nvpxenc keyframe header = %+v",
			got, want)
	}
}

func TestVP9EncoderVpxencOracleBlackKeyframeCompressedHeaderParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	govpxHeader, _ := parseVP9EncoderHeaderForTest(t, govpxPacket)
	libvpxHeader, _ := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
	if got, want := govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize; got != want {
		t.Fatalf("compressed header size = %d, want vpxenc %d", got, want)
	}

	govpxComp, govpxFc, govpxUncSize := readVP9CompressedHeaderForOracleTest(t,
		govpxPacket, govpxHeader)
	libvpxComp, libvpxFc, libvpxUncSize := readVP9CompressedHeaderForOracleTest(t,
		libvpxFrame.Data, libvpxHeader)
	if govpxComp != libvpxComp {
		t.Fatalf("compressed header = %+v, want vpxenc %+v", govpxComp, libvpxComp)
	}
	if govpxFc != libvpxFc {
		t.Fatalf("frame context after compressed header diverged from vpxenc")
	}

	govpxCompBytes := govpxPacket[govpxUncSize : govpxUncSize+int(govpxHeader.FirstPartitionSize)]
	libvpxCompBytes := libvpxFrame.Data[libvpxUncSize : libvpxUncSize+int(libvpxHeader.FirstPartitionSize)]
	if !bytes.Equal(govpxCompBytes, libvpxCompBytes) {
		t.Fatalf("compressed header bytes = % x, want vpxenc % x",
			govpxCompBytes, libvpxCompBytes)
	}
}

func TestVP9EncoderVpxencOracleBlackKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleMidgrayKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9YCbCrForTest(width, height, 128, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleCheckerKeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 16, 16
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func TestVP9EncoderVpxencOracleChecker64KeyframeByteParity(t *testing.T) {
	requireVP9VpxencOracle(t)

	const width, height = 64, 64
	src := newVP9CheckerYCbCrForTest(width, height, 32, 224, 128, 128)
	assertVP9VpxencKeyframeByteParity(t, src)
}

func assertVP9VpxencKeyframeByteParity(t *testing.T, src *image.YCbCr) {
	t.Helper()
	width := src.Rect.Dx()
	height := src.Rect.Dy()
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	govpxPacket, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode govpx keyframe: %v", err)
	}
	raw := appendVP9YCbCrI420(nil, src)
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height, 1)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		t.Fatalf("FirstIVFFrameOffset: %v", err)
	}
	libvpxFrame, _, err := testutil.NextIVFFrame(ivf, offset, 0)
	if err != nil {
		t.Fatalf("NextIVFFrame: %v", err)
	}

	if !bytes.Equal(govpxPacket, libvpxFrame.Data) {
		govpxHeader, govpxTileStart := parseVP9EncoderHeaderForTest(t, govpxPacket)
		libvpxHeader, libvpxTileStart := parseVP9EncoderHeaderForTest(t, libvpxFrame.Data)
		govpxGrid := decodeVP9PacketMiGridForOracleTest(t, govpxPacket)
		libvpxGrid := decodeVP9PacketMiGridForOracleTest(t, libvpxFrame.Data)
		govpxTx := decodeVP9PacketTxCoeffsForOracleTest(t, govpxPacket)
		libvpxTx := decodeVP9PacketTxCoeffsForOracleTest(t, libvpxFrame.Data)
		t.Fatalf("govpx header = %+v tileStart=%d tile=% x mi=%+v tx=%+v\nvpxenc header = %+v tileStart=%d tile=% x mi=%+v tx=%+v\ngovpx packet = % x\nvpxenc packet = % x",
			govpxHeader, govpxTileStart, govpxPacket[govpxTileStart:],
			govpxGrid, govpxTx,
			libvpxHeader, libvpxTileStart, libvpxFrame.Data[libvpxTileStart:],
			libvpxGrid, libvpxTx,
			govpxPacket, libvpxFrame.Data)
	}
}

type vp9OracleTxCoeffs struct {
	Plane   int
	Mode    common.PredictionMode
	TxSize  common.TxSize
	InitCtx int
	EOB     int
	Coeffs  []int16
}

func decodeVP9PacketTxCoeffsForOracleTest(t *testing.T, packet []byte) []vp9OracleTxCoeffs {
	t.Helper()
	hdr, tileStart := parseVP9EncoderHeaderForTest(t, packet)
	uncSize := tileStart - int(hdr.FirstPartitionSize)

	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:tileStart]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     hdr.Quant.Lossless,
		IntraOnly:    hdr.FrameType == common.KeyFrame || hdr.IntraOnly,
		KeyFrame:     hdr.FrameType == common.KeyFrame,
		InterpFilter: hdr.InterpFilter,
	})

	var r bitstream.Reader
	if err := r.Init(packet[tileStart:]); err != nil {
		t.Fatalf("tile reader Init: %v", err)
	}

	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	d.fc = fc
	vp9dec.SetupBlockPlanes(&d.planes, hdr.BitDepthColor.SubsamplingX,
		hdr.BitDepthColor.SubsamplingY)
	d.ensureVP9DecoderModeBuffers(miRows, miCols)
	d.resetVP9AboveEntropyContexts()
	d.resetVP9LeftEntropyContexts()
	vp9dec.SetupSegmentationDequant(&hdr.Seg, vp9dec.SetupSegmentationDequantArgs{
		BaseQindex: int(hdr.Quant.BaseQindex),
		YDcDeltaQ:  int(hdr.Quant.YDcDeltaQ),
		UvDcDeltaQ: int(hdr.Quant.UvDcDeltaQ),
		UvAcDeltaQ: int(hdr.Quant.UvAcDeltaQ),
		BitDepth:   vp9dec.BitDepth(hdr.BitDepthColor.BitDepth),
	}, &d.dq)
	maps := vp9dec.IntraSegmentMaps{
		CurrentFrameSegMap: d.segMap,
		LastFrameSegMap:    d.lastSegMap,
		MiCols:             miCols,
	}
	tile := vp9dec.TileBounds{MiRowStart: 0, MiRowEnd: miRows, MiColStart: 0, MiColEnd: miCols}
	out := make([]vp9OracleTxCoeffs, 0, 3)

	var collectBlock func(miRow, miCol int, bsize common.BlockSize)
	collectBlock = func(miRow, miCol int, bsize common.BlockSize) {
		xMis := min(int(common.Num8x8BlocksWideLookup[bsize]), miCols-miCol)
		yMis := min(int(common.Num8x8BlocksHighLookup[bsize]), miRows-miRow)
		mi := &d.miGrid[miRow*miCols+miCol]
		*mi = vp9dec.NeighborMi{SbType: bsize}
		above := d.vp9DecoderMiAt(miRows, miCols, miRow-1, miCol)
		var left *vp9dec.NeighborMi
		if miCol > tile.MiColStart {
			left = d.vp9DecoderMiAt(miRows, miCols, miRow, miCol-1)
		}
		modeOut := vp9dec.ReadIntraFrameModeInfo(vp9dec.IntraFrameDriverArgs{
			Reader:   &r,
			Fc:       &d.fc,
			Seg:      &hdr.Seg,
			Maps:     &maps,
			TxMode:   comp.TxMode,
			MiOffset: miRow*miCols + miCol,
			XMis:     xMis,
			YMis:     yMis,
			Above:    above,
			Left:     left,
		}, mi)
		reconBsize := vp9ModeInfoDecodeBSize(bsize)
		if mi.Skip != 0 {
			aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
			vp9dec.ResetSkipContext(d.planes[:], reconBsize, aboveOffsets[:], leftOffsets[:])
			d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
			return
		}
		out = append(out, collectVP9PacketResidueCoeffsForOracleTest(t, d, &r,
			&hdr, mi, modeOut.UvMode, miRow, miCol, reconBsize)...)
		d.fillVP9DecoderMiGrid(miRows, miCols, miRow, miCol, bsize, *mi)
	}
	var walk func(miRow, miCol int, bsize common.BlockSize)
	walk = func(miRow, miCol int, bsize common.BlockSize) {
		if miRow >= miRows || miCol >= miCols {
			return
		}
		bsl := int(common.BWidthLog2Lookup[bsize])
		bs := (1 << uint(bsl)) / 4
		ctx := vp9dec.PartitionPlaneContext(d.aboveSegCtx, d.leftSegCtx, miRow, miCol, bsize)
		probs := tables.KfPartitionProbs[ctx][:]
		hasRows := miRow+bs < miRows
		hasCols := miCol+bs < miCols
		partition := vp9dec.ReadPartition(&r, probs, hasRows, hasCols)
		subsize := common.SubsizeLookup[partition][bsize]
		switch partition {
		case common.PartitionNone:
			collectBlock(miRow, miCol, subsize)
		case common.PartitionHorz:
			collectBlock(miRow, miCol, subsize)
			if miRow+bs < miRows {
				collectBlock(miRow+bs, miCol, subsize)
			}
		case common.PartitionVert:
			collectBlock(miRow, miCol, subsize)
			if miCol+bs < miCols {
				collectBlock(miRow, miCol+bs, subsize)
			}
		case common.PartitionSplit:
			walk(miRow, miCol, subsize)
			walk(miRow, miCol+bs, subsize)
			walk(miRow+bs, miCol, subsize)
			walk(miRow+bs, miCol+bs, subsize)
		default:
			t.Fatalf("invalid partition %d", partition)
		}
		if bsize >= common.Block8x8 &&
			(bsize == common.Block8x8 || partition != common.PartitionSplit) {
			vp9dec.UpdatePartitionContext(d.aboveSegCtx, d.leftSegCtx,
				miRow, miCol, subsize, vp9PartitionContextUpdateWidth(bs))
		}
	}
	walk(0, 0, common.Block64x64)
	return out
}

func collectVP9PacketResidueCoeffsForOracleTest(t *testing.T, d *VP9Decoder,
	r *bitstream.Reader, hdr *vp9dec.UncompressedHeader, mi *vp9dec.NeighborMi,
	uvMode common.PredictionMode, miRow, miCol int, bsize common.BlockSize,
) []vp9OracleTxCoeffs {
	t.Helper()
	aboveOffsets, leftOffsets := d.vp9PlaneContextOffsets(miRow, miCol)
	miRows := int((hdr.Height + 7) >> 3)
	miCols := int((hdr.Width + 7) >> 3)
	out := make([]vp9OracleTxCoeffs, 0, 3)
	for plane := range vp9dec.MaxMbPlane {
		pd := &d.planes[plane]
		planeType := 0
		dequant := d.dq.Y[mi.SegmentID]
		if plane > 0 {
			planeType = 1
			dequant = d.dq.Uv[mi.SegmentID]
		}
		txSize := mi.TxSize
		if plane > 0 {
			txSize = vp9dec.GetUvTxSize(bsize, mi.TxSize, pd)
		}
		planeBsize := vp9dec.GetPlaneBlockSize(bsize, pd)
		full4x4W := int(common.Num4x4BlocksWideLookup[planeBsize])
		num4x4W, num4x4H := vp9PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
			bsize, pd, planeBsize)
		step := 1 << uint(txSize)
		blockStep := 1 << uint(txSize<<1)
		extraStep := ((full4x4W - num4x4W) >> txSize) * blockStep
		aboveBase := aboveOffsets[plane]
		leftBase := leftOffsets[plane]
		blockIdx := 0
		for rr := 0; rr < num4x4H; rr += step {
			for cc := 0; cc < num4x4W; cc += step {
				mode := uvMode
				if plane == 0 {
					mode = vp9dec.GetYMode(mi, blockIdx)
				}
				aboveCtx := pd.AboveContext[aboveBase+cc : aboveBase+cc+step]
				leftCtx := pd.LeftContext[leftBase+rr : leftBase+rr+step]
				initCtx := vp9dec.GetEntropyContext(txSize, aboveCtx, leftCtx)
				scanOrder := common.GetScan(txSize, planeType, 0,
					hdr.Quant.Lossless, mode)
				maxEob := vp9dec.MaxEobForTxSize(txSize)
				coeffs := make([]int16, maxEob)
				eob := vp9dec.DecodeCoefs(r, txSize, planeType, 0, dequant,
					initCtx, scanOrder.Scan, scanOrder.Neighbors, &d.fc.CoefProbs,
					coeffs)
				out = append(out, vp9OracleTxCoeffs{
					Plane:   plane,
					Mode:    mode,
					TxSize:  txSize,
					InitCtx: initCtx,
					EOB:     eob,
					Coeffs:  coeffs,
				})
				hasResidue := uint8(0)
				if eob > 0 {
					hasResidue = 1
				}
				for i := range step {
					aboveCtx[i] = hasResidue
					leftCtx[i] = hasResidue
				}
				blockIdx += blockStep
			}
			blockIdx += extraStep
		}
	}
	return out
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

func readVP9CompressedHeaderForOracleTest(t *testing.T, packet []byte,
	header vp9dec.UncompressedHeader,
) (vp9dec.CompressedHeader, vp9dec.FrameContext, int) {
	t.Helper()
	var br vp9dec.BitReader
	br.Init(packet)
	if _, err := vp9dec.ReadUncompressedHeader(&br, nil, nil); err != nil {
		t.Fatalf("ReadUncompressedHeader: %v", err)
	}
	uncSize := br.BytesRead()
	compEnd := uncSize + int(header.FirstPartitionSize)
	var cr bitstream.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:     header.Quant.Lossless,
		IntraOnly:    header.FrameType == common.KeyFrame || header.IntraOnly,
		KeyFrame:     header.FrameType == common.KeyFrame,
		InterpFilter: header.InterpFilter,
	})
	return comp, fc, uncSize
}
