//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"testing"
)

func formatVP9StreamParityRows(t *testing.T, govpxPackets, libvpxPackets [][]byte) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,match,first_diff,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part,govpx_unc,libvpx_unc,govpx_tile_start,libvpx_tile_start,govpx_seg,libvpx_seg,govpx_seg_map,libvpx_seg_map,govpx_seg_data,libvpx_seg_data,govpx_seg_temporal,libvpx_seg_temporal")
	for i := range govpxPackets {
		govpxHeader, govpxTileStart := parseVP9EncoderHeaderForTest(t,
			govpxPackets[i])
		libvpxHeader, libvpxTileStart := parseVP9EncoderHeaderForTest(t,
			libvpxPackets[i])
		govpxUncompressed := govpxTileStart - int(govpxHeader.FirstPartitionSize)
		libvpxUncompressed := libvpxTileStart - int(libvpxHeader.FirstPartitionSize)
		fmt.Fprintf(&b, "%d,%t,%d,%d,%d,%d,%d,%#x,%#x,%d,%d,%d,%d,%d,%d,%t,%t,%t,%t,%t,%t,%t,%t\n",
			i, bytes.Equal(govpxPackets[i], libvpxPackets[i]),
			firstVP9PacketDiffForTest(govpxPackets[i], libvpxPackets[i]),
			len(govpxPackets[i]), len(libvpxPackets[i]),
			govpxHeader.Quant.BaseQindex, libvpxHeader.Quant.BaseQindex,
			govpxHeader.RefreshFrameFlags, libvpxHeader.RefreshFrameFlags,
			govpxHeader.FirstPartitionSize, libvpxHeader.FirstPartitionSize,
			govpxUncompressed, libvpxUncompressed, govpxTileStart,
			libvpxTileStart, govpxHeader.Seg.Enabled, libvpxHeader.Seg.Enabled,
			govpxHeader.Seg.UpdateMap, libvpxHeader.Seg.UpdateMap,
			govpxHeader.Seg.UpdateData, libvpxHeader.Seg.UpdateData,
			govpxHeader.Seg.TemporalUpdate, libvpxHeader.Seg.TemporalUpdate)
	}
	return b.String()
}

func formatVP9DropAwareStreamParityRows(t *testing.T,
	govpxRows []vp9RateScoreboardRow, govpxPackets [][]byte,
	libvpxRows []vp9RateScoreboardRow, libvpxPackets [][]byte,
) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,row_match,packet_match,first_diff,govpx_drop,libvpx_drop,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_target,libvpx_target,govpx_buffer,libvpx_buffer,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part")
	for i := range govpxRows {
		g := govpxRows[i]
		l := libvpxRows[i]
		packetMatch := false
		if g.Dropped && l.Dropped {
			packetMatch = true
		} else if !g.Dropped && !l.Dropped {
			packetMatch = bytes.Equal(govpxPackets[i], libvpxPackets[i])
		}
		rowMatch := g.Dropped == l.Dropped &&
			g.BaseQIndex == l.BaseQIndex &&
			g.FrameTargetBits == l.FrameTargetBits &&
			g.BufferLevelBits == l.BufferLevelBits &&
			g.RefreshFrameFlags == l.RefreshFrameFlags
		fmt.Fprintf(&b, "%d,%t,%t,%d,%t,%t,%d,%d,%d,%d,%d,%d,%d,%d,%#x,%#x,%d,%d\n",
			g.FrameIndex, rowMatch, packetMatch,
			firstVP9PacketDiffForTest(govpxPackets[i], libvpxPackets[i]),
			g.Dropped, l.Dropped,
			len(govpxPackets[i]), len(libvpxPackets[i]), g.BaseQIndex,
			l.BaseQIndex, g.FrameTargetBits, l.FrameTargetBits,
			g.BufferLevelBits, l.BufferLevelBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.FirstPartitionSize, l.FirstPartitionSize)
	}
	return b.String()
}
