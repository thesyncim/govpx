package vp9test

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func BuildVP9IVF(width, height int, packets ...[]byte) []byte {
	return testutil.BuildVP9IVF(width, height, 30, 1, packets)
}

func MD5Hex(data []byte) string {
	return testutil.MD5Hex(md5.Sum(data))
}

func ParseIVFFrames(t testing.TB, data []byte) [][]byte {
	t.Helper()
	out, err := testutil.IVFFramePayloads(data)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	return out
}

func AssertSegmentByteParity(t testing.TB, label string, got, want [][]byte, matchLimit int) {
	t.Helper()
	if len(got) != len(want) {
		if matchLimit < 0 {
			t.Logf("%s: frame count mismatch (logged only): got=%d want=%d",
				label, len(got), len(want))
		} else {
			t.Errorf("%s: frame count mismatch: got=%d want=%d",
				label, len(got), len(want))
			if matchLimit == 0 {
				return
			}
		}
	}

	limit := len(got)
	if matchLimit < 0 {
		limit = 0
	} else if matchLimit > 0 && matchLimit < limit {
		limit = matchLimit
	}

	common := min(len(got), len(want))
	for i := range common {
		gotHash := sha256.Sum256(got[i])
		wantHash := sha256.Sum256(want[i])
		if gotHash == wantHash {
			t.Logf("%s frame %d byte MATCH: len=%d", label, i, len(got[i]))
			continue
		}
		firstDiff := testutil.FirstByteDiff(got[i], want[i])
		if i >= limit {
			t.Logf("%s frame %d byte mismatch (not asserted, limit=%d): got_len=%d want_len=%d first_diff=%d got_sha=%s want_sha=%s",
				label, i, limit, len(got[i]), len(want[i]), firstDiff,
				hex.EncodeToString(gotHash[:8]),
				hex.EncodeToString(wantHash[:8]))
			continue
		}
		t.Errorf("%s frame %d byte mismatch: got_len=%d want_len=%d first_diff=%d got_sha=%s want_sha=%s",
			label, i, len(got[i]), len(want[i]), firstDiff,
			hex.EncodeToString(gotHash[:8]),
			hex.EncodeToString(wantHash[:8]))
	}
}

func AssertPacketByteParity(t testing.TB, label string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	gotHeader, gotTileStart := ParseHeader(t, got)
	wantHeader, wantTileStart := ParseHeader(t, want)
	t.Fatalf("%s packet diverged firstDiff=%d\ngovpx header=%+v tileStart=%d tile=% x\nvpxenc header=%+v tileStart=%d tile=% x\ngovpx packet=% x\nvpxenc packet=% x",
		label, testutil.FirstByteDiff(got, want),
		gotHeader, gotTileStart, got[gotTileStart:],
		wantHeader, wantTileStart, want[wantTileStart:],
		got, want)
}

func NewPanningSources(width, height, frames int) []*image.YCbCr {
	out := make([]*image.YCbCr, frames)
	for i := range out {
		out[i] = NewPanningYCbCr(width, height, i)
	}
	return out
}

func NewSteppedSources(width, height, frames int) []*image.YCbCr {
	out := make([]*image.YCbCr, frames)
	for i := range out {
		out[i] = NewYCbCr(width, height, uint8(96+i*8), 128, 128)
	}
	return out
}

func NewBlockCheckerYCbCr(width, height, frame int) *image.YCbCr {
	img := NewYCbCr(width, height, 128, 128, 128)
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			if ((x>>5)+(y>>5)+frame)&1 == 0 {
				row[x] = 96
			} else {
				row[x] = 160
			}
		}
	}
	return img
}

func NewRuntimeResizeSources(w0, h0, w1, h1, resizeFrame, frames int) []*image.YCbCr {
	out := make([]*image.YCbCr, frames)
	for i := range out {
		width, height := w0, h0
		if i >= resizeFrame {
			width, height = w1, h1
		}
		out[i] = NewPanningYCbCr(width, height, i)
	}
	return out
}

func CountByteParityMatches(got, want [][]byte) (matches int, firstMismatch int) {
	firstMismatch = -1
	for i := range got {
		if bytes.Equal(got[i], want[i]) {
			matches++
			continue
		}
		if firstMismatch < 0 {
			firstMismatch = i
		}
	}
	return matches, firstMismatch
}

func RequireIVFPackets(t testing.TB, data []byte, wantPackets int) [][]byte {
	t.Helper()
	packets, err := testutil.IVFFramePayloads(data)
	if err != nil {
		t.Fatalf("IVFFramePayloads: %v", err)
	}
	if len(packets) != wantPackets {
		t.Fatalf("IVF frame count = %d, want %d", len(packets), wantPackets)
	}
	return packets
}

func FormatStreamParityRows(t testing.TB, govpxPackets, libvpxPackets [][]byte) string {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintln(&b, "frame,match,first_diff,govpx_bytes,libvpx_bytes,govpx_q,libvpx_q,govpx_refresh,libvpx_refresh,govpx_first_part,libvpx_first_part,govpx_unc,libvpx_unc,govpx_tile_start,libvpx_tile_start,govpx_seg,libvpx_seg,govpx_seg_map,libvpx_seg_map,govpx_seg_data,libvpx_seg_data,govpx_seg_temporal,libvpx_seg_temporal")
	for i := range govpxPackets {
		govpxHeader, govpxTileStart := ParseHeader(t, govpxPackets[i])
		libvpxHeader, libvpxTileStart := ParseHeader(t, libvpxPackets[i])
		govpxUncompressed := govpxTileStart - int(govpxHeader.FirstPartitionSize)
		libvpxUncompressed := libvpxTileStart - int(libvpxHeader.FirstPartitionSize)
		fmt.Fprintf(&b, "%d,%t,%d,%d,%d,%d,%d,%#x,%#x,%d,%d,%d,%d,%d,%d,%t,%t,%t,%t,%t,%t,%t,%t\n",
			i, bytes.Equal(govpxPackets[i], libvpxPackets[i]),
			testutil.FirstByteDiff(govpxPackets[i], libvpxPackets[i]),
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
