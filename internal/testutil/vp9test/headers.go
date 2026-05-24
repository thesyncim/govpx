package vp9test

import (
	"testing"

	vp9bits "github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9common "github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
)

func ReadCompressedHeader(t testing.TB, packet []byte,
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
	var cr vp9bits.Reader
	if err := cr.Init(packet[uncSize:compEnd]); err != nil {
		t.Fatalf("compressed reader Init: %v", err)
	}
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)
	compoundAllowed := header.FrameType != vp9common.KeyFrame && !header.IntraOnly &&
		vp9dec.CompoundReferenceAllowed(vp9dec.FrameRefSignBias(&header))
	comp := vp9dec.ReadCompressedHeader(&cr, &fc, vp9dec.ReadCompressedHeaderArgs{
		Lossless:             header.Quant.Lossless,
		IntraOnly:            header.FrameType == vp9common.KeyFrame || header.IntraOnly,
		KeyFrame:             header.FrameType == vp9common.KeyFrame,
		InterpFilter:         header.InterpFilter,
		AllowHighPrecisionMv: header.AllowHighPrecisionMv,
		CompoundRefAllowed:   compoundAllowed,
	})
	return comp, fc, uncSize
}

func EnrichRateScoreboardRowFromPacket(t testing.TB, row *RateScoreboardRow, packet []byte) {
	t.Helper()
	header, _ := ParseHeader(t, packet)
	comp, _, _ := ReadCompressedHeader(t, packet, header)
	row.KeyFrame = header.FrameType == vp9common.KeyFrame
	row.ShowFrame = header.ShowFrame
	if header.Width != 0 {
		row.CodedWidth = int(header.Width)
	}
	if header.Height != 0 {
		row.CodedHeight = int(header.Height)
	}
	row.BaseQIndex = int(header.Quant.BaseQindex)
	row.PublicQuantizer = vp9enc.QIndexToPublicQuantizer(int(header.Quant.BaseQindex))
	row.SizeBytes = len(packet)
	row.SizeBits = len(packet) * 8
	row.FirstPartitionSize = int(header.FirstPartitionSize)
	row.RefreshFrameFlags = header.RefreshFrameFlags
	row.RefreshFrameContext = header.RefreshFrameContext
	row.ErrorResilient = header.ErrorResilientMode
	row.FrameParallel = header.FrameParallelDecoding
	row.FrameContextIdx = int(header.FrameContextIdx)
	row.TxMode = int(comp.TxMode)
	row.InterpFilter = int(header.InterpFilter)
	row.ReferenceMode = int(comp.ReferenceMode)
	row.CompoundAllowed = header.FrameType != vp9common.KeyFrame && !header.IntraOnly &&
		vp9dec.CompoundReferenceAllowed(vp9dec.FrameRefSignBias(&header))
	row.ReferenceMask = ReferenceMaskFromLibvpxFrameFlags(row.Flags)
	row.LoopFilterLevel = int(header.Loopfilter.FilterLevel)
	row.TileLog2Cols = int(header.Tile.Log2TileCols)
	row.TileLog2Rows = int(header.Tile.Log2TileRows)
}

func ReferenceMaskFromLibvpxFrameFlags(flags uint32) uint8 {
	const (
		libvpxNoRefLast = 1 << 16
		libvpxNoRefGF   = 1 << 17
		libvpxNoRefARF  = 1 << 21
	)
	var mask uint8
	if flags&libvpxNoRefLast == 0 {
		mask |= 1 << uint(vp9dec.LastFrame)
	}
	if flags&libvpxNoRefGF == 0 {
		mask |= 1 << uint(vp9dec.GoldenFrame)
	}
	if flags&libvpxNoRefARF == 0 {
		mask |= 1 << uint(vp9dec.AltrefFrame)
	}
	return mask
}
