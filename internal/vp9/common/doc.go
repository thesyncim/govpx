// Package common holds VP9 state shared between the decoder and encoder:
// frame headers, sequence parameters, segmentation, reference-frame
// management, motion-vector references, partition tree state, loop-filter
// parameters, and the common-data tables that drive intra/inter prediction.
//
// VP9 frames are tiled (1..64 superblock cols, 1..4 rows of full-height
// tiles) and built from a recursive partition tree rooted at 64x64
// superblocks. This package owns the data structures that describe that
// tree plus its surrounding reference frame state; the decoder and
// encoder packages drive it.
//
// Upstream:
//
//	libvpx v1.16.0 vp9/common/{vp9_blockd,vp9_onyxc_int,vp9_mvref_common,
//	vp9_alloccommon,vp9_pred_common,vp9_common_data,vp9_quant_common,
//	vp9_loopfilter,vp9_filter,vp9_idct,vp9_reconinter,vp9_reconintra}.{c,h}
package common
