//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func formatVP9DropAwareStreamParityRows(t *testing.T,
	govpxRows []vp9test.RateScoreboardRow, govpxPackets [][]byte,
	libvpxRows []vp9test.RateScoreboardRow, libvpxPackets [][]byte,
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
			vp9test.FirstPacketDiff(govpxPackets[i], libvpxPackets[i]),
			g.Dropped, l.Dropped,
			len(govpxPackets[i]), len(libvpxPackets[i]), g.BaseQIndex,
			l.BaseQIndex, g.FrameTargetBits, l.FrameTargetBits,
			g.BufferLevelBits, l.BufferLevelBits, g.RefreshFrameFlags,
			l.RefreshFrameFlags, g.FirstPartitionSize, l.FirstPartitionSize)
	}
	return b.String()
}
