//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
	"testing"
)

func decodeVP9MiGridForOracleTest(t *testing.T, packet []byte) []vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode VP9 packet: %v", err)
	}
	grid := make([]vp9dec.NeighborMi, len(d.miGrid))
	copy(grid, d.miGrid)
	return grid
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
	hdr, tileStart := vp9test.ParseHeader(t, packet)
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
		reconBsize := vp9dec.ModeInfoDecodeBSize(bsize)
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
				miRow, miCol, subsize, vp9dec.PartitionContextUpdateWidth(bs))
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
		num4x4W, num4x4H := vp9dec.PlaneMaxBlocks4x4(miRows, miCols, miRow, miCol,
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

func decodeVP9SequenceMiGridsForOracleTest(t *testing.T, packets [][]byte) [][]vp9dec.NeighborMi {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	out := make([][]vp9dec.NeighborMi, len(packets))
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode packet %d: %v", i, err)
		}
		out[i] = make([]vp9dec.NeighborMi, len(d.miGrid))
		copy(out[i], d.miGrid)
		if i+1 < len(packets) {
			if _, ok := d.NextFrame(); !ok {
				t.Fatalf("NextFrame returned !ok after packet %d", i)
			}
		}
	}
	return out
}

type vp9ModeDistributionForOracleTest struct {
	Total  int
	Skip   int
	Modes  [common.MbModeCount]int
	Blocks [common.BlockSizes]int
}

func collectVP9ModeDistribution(grid []vp9dec.NeighborMi) vp9ModeDistributionForOracleTest {
	var dist vp9ModeDistributionForOracleTest
	for i := range grid {
		mi := &grid[i]
		dist.Total++
		if mi.Skip != 0 {
			dist.Skip++
		}
		if mode := int(mi.Mode); mode >= 0 && mode < len(dist.Modes) {
			dist.Modes[mode]++
		}
		if block := int(mi.SbType); block >= 0 && block < len(dist.Blocks) {
			dist.Blocks[block]++
		}
	}
	return dist
}

func (dist vp9ModeDistributionForOracleTest) String() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "total=%d skip=%d modes=[", dist.Total, dist.Skip)
	writeVP9IntHistogramForOracleTest(&b, dist.Modes[:])
	b.WriteString("] blocks=[")
	writeVP9IntHistogramForOracleTest(&b, dist.Blocks[:])
	b.WriteByte(']')
	return b.String()
}

func writeVP9IntHistogramForOracleTest(b *bytes.Buffer, hist []int) {
	first := true
	for i, count := range hist {
		if count == 0 {
			continue
		}
		if !first {
			b.WriteByte(' ')
		}
		fmt.Fprintf(b, "%d:%d", i, count)
		first = false
	}
	if first {
		b.WriteString("empty")
	}
}

func vp9ModeDistributionDistance(a, b [common.MbModeCount]int) int {
	distance := 0
	for i := range a {
		distance += vp9AbsIntForOracleTest(a[i] - b[i])
	}
	return distance
}

func vp9BlockDistributionDistance(a, b [common.BlockSizes]int) int {
	distance := 0
	for i := range a {
		distance += vp9AbsIntForOracleTest(a[i] - b[i])
	}
	return distance
}

func vp9AbsIntForOracleTest(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func firstLastVP9MiForOracleTest(grid []vp9dec.NeighborMi) (vp9dec.NeighborMi, vp9dec.NeighborMi) {
	if len(grid) == 0 {
		return vp9dec.NeighborMi{}, vp9dec.NeighborMi{}
	}
	return grid[0], grid[len(grid)-1]
}
