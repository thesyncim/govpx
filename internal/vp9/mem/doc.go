// Package mem holds VP9 frame buffer and arena helpers: macroblock-padded
// border-addressable I420 / I422 / I444 buffers, reference-frame pools
// with per-pool reuse, and tile-local scratch arenas used by the decoder
// and encoder to avoid per-frame allocations in steady state.
//
// Upstream:
//
//	libvpx v1.16.0 vpx_scale/{yv12*,vpx_scale}.{c,h};
//	vpx_mem/vpx_mem.{c,h}; vp9/common/vp9_frame_buffers.{c,h}.
package mem
