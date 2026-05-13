package decoder

import "github.com/thesyncim/govpx/internal/vp9/common"

// VP9 per-plane shape helpers. Ported from libvpx v1.16.0
// vp9/common/vp9_blockd.h — get_plane_block_size, get_uv_tx_size,
// reset_skip_context and the supporting macroblockd_plane shape.

// MaxMbPlane mirrors libvpx's MAX_MB_PLANE — 3 planes (Y, U, V).
const MaxMbPlane = 3

// MacroblockdPlane mirrors the parser-visible subset of libvpx's
// struct macroblockd_plane: the chroma-subsampling pair (0 for luma
// or 4:4:4 chroma, 1 for the subsampled chroma axes) plus the two
// entropy-context buffers a tile pass updates per super-block row.
//
// Caller owns AboveContext and LeftContext; the per-frame setup
// allocates them sized to the frame's column count (above) and
// MI_BLOCK_SIZE (left).
type MacroblockdPlane struct {
	SubsamplingX uint8
	SubsamplingY uint8

	// EntropyContext arrays carry per-4x4-block "has-residual" bits
	// for the coefficient-context update path. ENTROPY_CONTEXT is
	// uint8 in libvpx.
	AboveContext []uint8
	LeftContext  []uint8
}

// GetPlaneBlockSize mirrors get_plane_block_size — projects a luma
// BlockSize onto the chroma plane using the (subsampling_x,
// subsampling_y) pair from the plane.
func GetPlaneBlockSize(bsize common.BlockSize, pd *MacroblockdPlane) common.BlockSize {
	if bsize >= common.BlockSizes || pd.SubsamplingX > 1 || pd.SubsamplingY > 1 {
		return common.BlockInvalid
	}
	return common.SsSizeLookup[bsize][pd.SubsamplingX][pd.SubsamplingY]
}

// GetUvTxSize mirrors get_uv_tx_size — picks the chroma plane's
// transform size from the (sb_type, luma tx_size) pair via the
// per-subsampling UV lookup table.
func GetUvTxSize(sbType common.BlockSize, lumaTxSize common.TxSize, pd *MacroblockdPlane) common.TxSize {
	if sbType >= common.BlockSizes || lumaTxSize >= common.TxSizes ||
		pd.SubsamplingX > 1 || pd.SubsamplingY > 1 {
		return common.Tx4x4
	}
	return common.UvTxsizeLookup[sbType][lumaTxSize][pd.SubsamplingX][pd.SubsamplingY]
}

// GetEntropyContext mirrors libvpx's get_entropy_context. Combines
// the above and left entropy-context windows for the given tx size
// — non-zero in any byte makes that axis "1", and the result is the
// sum across axes (0..2). The window width is (1 << txSize) bytes
// (1 for 4x4, 2 for 8x8, 4 for 16x16, 8 for 32x32).
func GetEntropyContext(txSize common.TxSize, aboveCtx, leftCtx []uint8) int {
	n := 1 << uint(txSize)
	above := 0
	for i := 0; i < n && i < len(aboveCtx); i++ {
		if aboveCtx[i] != 0 {
			above = 1
			break
		}
	}
	left := 0
	for i := 0; i < n && i < len(leftCtx); i++ {
		if leftCtx[i] != 0 {
			left = 1
			break
		}
	}
	return above + left
}

// ResetSkipContext mirrors reset_skip_context. After a skip block,
// libvpx zeros the matching window of the above + left entropy
// context buffers so the coefficient-context cache doesn't carry
// non-zero residue forward.
func ResetSkipContext(planes []MacroblockdPlane, bsize common.BlockSize, aboveOffsets, leftOffsets []int) {
	for i := range planes {
		pd := &planes[i]
		planeBsize := GetPlaneBlockSize(bsize, pd)
		if planeBsize >= common.BlockSizes {
			continue
		}
		bw := int(common.Num4x4BlocksWideLookup[planeBsize])
		bh := int(common.Num4x4BlocksHighLookup[planeBsize])
		if i < len(aboveOffsets) && len(pd.AboveContext) >= aboveOffsets[i]+bw {
			for j := range bw {
				pd.AboveContext[aboveOffsets[i]+j] = 0
			}
		}
		if i < len(leftOffsets) && len(pd.LeftContext) >= leftOffsets[i]+bh {
			for j := range bh {
				pd.LeftContext[leftOffsets[i]+j] = 0
			}
		}
	}
}

// SetupBlockPlanes mirrors vp9_setup_block_planes — assigns the
// (ssX, ssY) pair to the chroma planes (slot 0 is luma; slots 1 and
// 2 carry the subsampling for U / V). The above/left context buffers
// are caller-supplied (or left nil for tests that don't drive the
// coefficient-context update path).
func SetupBlockPlanes(planes *[MaxMbPlane]MacroblockdPlane, ssX, ssY uint8) {
	for i := range planes {
		if i == 0 {
			planes[i].SubsamplingX = 0
			planes[i].SubsamplingY = 0
		} else {
			planes[i].SubsamplingX = ssX
			planes[i].SubsamplingY = ssY
		}
	}
}

// FramePlaneDims projects a frame's luma (Y) dimensions onto the
// chroma plane using the libvpx convention: chroma dims = ceil(luma
// / 2^ss). Used by the inter-pred frame-buffer reach to compute
// the correct cropping bounds.
func FramePlaneDims(yW, yH int, ssX, ssY uint8) (uvW, uvH int) {
	uvW = (yW + (1 << ssX) - 1) >> ssX
	uvH = (yH + (1 << ssY) - 1) >> ssY
	return
}
