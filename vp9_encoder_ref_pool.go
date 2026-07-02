package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

// This file ports libvpx's encoder-side reference frame buffer management:
// a small pool of reconstruction buffers with reference counts, where the
// LAST/GOLDEN/ALTREF map entries are pointers into the pool rather than
// per-slot pixel copies.
//
// libvpx ground truth:
//   - vp9/common/vp9_onyxc_int.h:36-41 — REF_FRAMES / FRAME_BUFFERS
//     (FRAME_BUFFERS = REF_FRAMES + 1 + REFS_PER_FRAME = 12) and the
//     BufferPool's RefCntBuffer frame_bufs[FRAME_BUFFERS].
//   - vp9/common/vp9_onyxc_int.h:331-346 get_free_fb — scan for the first
//     frame buffer with ref_count == 0 and claim it with ref_count = 1.
//   - vp9/common/vp9_onyxc_int.h:348-357 ref_cnt_fb — release the buffer a
//     ref_frame_map slot previously pointed at and point it at new_fb_idx,
//     incrementing that buffer's count. No pixels are copied.
//   - vp9/encoder/vp9_encoder.c:6315-6319 (vp9_get_compressed_data) — at the
//     start of each frame the encoder releases the reference it held on the
//     previous cm->new_fb_idx and acquires a fresh free buffer, so the new
//     reconstruction is never written into a buffer a live reference aliases.
//   - vp9/encoder/vp9_encoder.c:3324-3395 update_ref_frames /
//     vp9_update_reference_frames — after the frame is encoded, each
//     refreshed ref_frame_map slot is repointed at cm->new_fb_idx via
//     ref_cnt_fb.
//
// The pool is owned exclusively by the encode goroutine: buffers are
// acquired/released at frame boundaries and at the post-encode reference
// refresh, never from tile or frame-parallel workers. Clones drop the pool
// pointer (see prepareVP9FrameParallelWorker / prepareVP9TileEncodeWorker)
// so their fallback paths keep the historical copy semantics.

// vp9EncoderFrameBufferCount mirrors libvpx FRAME_BUFFERS
// (vp9/common/vp9_onyxc_int.h:41): REF_FRAMES + 1 + REFS_PER_FRAME.
const vp9EncoderFrameBufferCount = common.RefFrames + 1 + 3

// vp9EncoderFrameBuffer is one RefCntBuffer worth of reconstruction storage.
// The planes use the same padded FrameLayout as the encoder's working
// reconstruction buffers; img is the visible-plane view (Y at
// layout.YOrigin, U/V at layout.UVOrigin) that reference slots alias.
type vp9EncoderFrameBuffer struct {
	yFull []byte
	uFull []byte
	vFull []byte
	img   Image
}

// vp9EncoderFramePool is the encoder-side BufferPool analog
// (vpx/internal/vpx_codec_internal.h / vp9_onyxc_int.h BufferPool with
// RefCntBuffer frame_bufs[FRAME_BUFFERS]).
type vp9EncoderFramePool struct {
	frames   [vp9EncoderFrameBufferCount]vp9EncoderFrameBuffer
	refCount [vp9EncoderFrameBufferCount]int
}

// vp9EncoderFramePoolWarmEntries is how many pool entries
// prepareVP9EncoderOutputFrame keeps pre-sized for the active frame layout.
// The realtime rotation pattern touches at most this many distinct buffers
// (one pinned by the last keyframe's GOLDEN/ALTREF aliases, a LAST/recon
// ping-pong pair, plus one for an independent GOLDEN/ALTREF refresh), so
// pre-sizing them keeps the steady-state encode allocation-free from the
// first inter frame — the same standing the old copy-based store had, where
// the recon buffer plus three per-slot copies were all sized by the first
// keyframe. Entries beyond the warm set (deep SVC slot configurations) are
// still sized lazily on first acquire.
const vp9EncoderFramePoolWarmEntries = 4

// freeIndex ports get_free_fb's scan (vp9_onyxc_int.h:331-346) without
// claiming the buffer; callers set the initial reference count explicitly so
// the claim discipline stays readable at each call site.
func (p *vp9EncoderFramePool) freeIndex() int {
	for i := range p.refCount {
		if p.refCount[i] == 0 {
			return i
		}
	}
	return -1
}

func (e *VP9Encoder) initVP9EncoderFramePool() {
	e.reconPool = &vp9EncoderFramePool{}
	e.reconPoolIdx = -1
	for i := range e.refPoolIdx {
		e.refPoolIdx[i] = -1
	}
}

// acquireVP9ReconFrameBuffer rotates the working reconstruction target onto
// a pool buffer no live reference aliases. Ports the top of
// vp9_get_compressed_data (vp9/encoder/vp9_encoder.c:6315-6319):
//
//	if (cm->new_fb_idx != INVALID_IDX) --pool->frame_bufs[cm->new_fb_idx].ref_count;
//	cm->new_fb_idx = get_free_fb(cm);
//
// Invoked from prepareVP9EncoderOutputFrame — the single site that arms the
// reconstruction planes for writing — so the libvpx invariant "the encode
// never writes into a buffer a live reference aliases" is enforced at the
// writer. When the working buffer's only reference is the encoder's own
// (ref_count == 1, no map slot aliases it) the buffer is reused in place,
// which is what get_free_fb's ref_count==0 scan degenerates to after the
// release. A nil pool (worker clones) leaves the historical single-buffer
// recon in place; refreshVP9EncoderRefs then falls back to the copy store.
func (e *VP9Encoder) acquireVP9ReconFrameBuffer() {
	pool := e.reconPool
	if pool == nil {
		return
	}
	if e.reconPoolIdx >= 0 {
		if pool.refCount[e.reconPoolIdx] == 1 {
			// Sole owner: no reference-map slot took the buffer, so the
			// release-then-reacquire round trip would hand back a buffer
			// with the same standing. Keep writing it.
			return
		}
		if pool.refCount[e.reconPoolIdx] > 0 {
			pool.refCount[e.reconPoolIdx]--
		}
	}
	e.reconPoolIdx = -1
	idx := pool.freeIndex()
	if idx < 0 {
		// Cannot happen with FRAME_BUFFERS = REF_FRAMES + 1 + REFS_PER_FRAME
		// (at most RefFrames map slots plus the working buffer are live), but
		// keep the copy-based fallback rather than corrupting a live ref.
		return
	}
	pool.refCount[idx] = 1
	e.reconPoolIdx = idx
	ent := &pool.frames[idx]
	e.reconYFull = ent.yFull
	e.reconUFull = ent.uFull
	e.reconVFull = ent.vFull
}

// warmVP9EncoderFramePool pre-sizes the leading pool entries for the given
// layout so steady-state buffer rotation never allocates. Growth-only, like
// every other frame-scoped Ensure helper; entries keep their pixels.
func (e *VP9Encoder) warmVP9EncoderFramePool(layout common.FrameLayout) {
	pool := e.reconPool
	if pool == nil {
		return
	}
	for i := range vp9EncoderFramePoolWarmEntries {
		ent := &pool.frames[i]
		ent.yFull = buffers.EnsureAlignedCapacity(ent.yFull, layout.YFullLen, 32)
		ent.uFull = buffers.EnsureAlignedCapacity(ent.uFull, layout.UVFullLen, 32)
		ent.vFull = buffers.EnsureAlignedCapacity(ent.vFull, layout.UVFullLen, 32)
	}
	if e.reconPoolIdx >= 0 && e.reconPoolIdx < vp9EncoderFramePoolWarmEntries {
		// Re-adopt the working entry's arrays in case the growth-only
		// sizing above replaced them.
		ent := &pool.frames[e.reconPoolIdx]
		e.reconYFull = ent.yFull
		e.reconUFull = ent.uFull
		e.reconVFull = ent.vFull
	}
}

// syncVP9ReconPoolBacking records the (possibly grown) reconstruction
// backing arrays and visible-plane view on the pool entry the encoder is
// currently writing. libvpx's analog is vp9_realloc_frame_buffer growing
// frame_bufs[new_fb_idx].buf in place; govpx sizes through
// prepareVP9EncoderOutputFrame's EnsureAlignedCapacity, so the entry must
// re-adopt the arrays afterwards.
func (e *VP9Encoder) syncVP9ReconPoolBacking() {
	if e.reconPool == nil || e.reconPoolIdx < 0 {
		return
	}
	ent := &e.reconPool.frames[e.reconPoolIdx]
	ent.yFull = e.reconYFull
	ent.uFull = e.reconUFull
	ent.vFull = e.reconVFull
	ent.img = e.reconFrame
}

// aliasVP9EncoderRefToRecon points reference-map slot at the working
// reconstruction buffer, porting ref_cnt_fb(pool->frame_bufs,
// &cm->ref_frame_map[slot], cm->new_fb_idx) from update_ref_frames
// (vp9/encoder/vp9_encoder.c:3324-3388): release the buffer the slot held,
// repoint the slot, bump the new buffer's count. Returns false when the pool
// is unavailable so refreshVP9EncoderRefs can fall back to the copy store.
func (e *VP9Encoder) aliasVP9EncoderRefToRecon(slot int) bool {
	pool := e.reconPool
	if pool == nil || e.reconPoolIdx < 0 ||
		slot < 0 || slot >= len(e.refPoolIdx) {
		return false
	}
	old := e.refPoolIdx[slot]
	if old >= 0 && pool.refCount[old] > 0 {
		pool.refCount[old]--
	}
	e.refPoolIdx[slot] = e.reconPoolIdx
	pool.refCount[e.reconPoolIdx]++
	e.bindVP9EncoderRefView(slot, e.reconFrame)
	return true
}

// bindVP9EncoderRefView makes refFrames[slot] a view of src without copying
// pixels; the Image slice headers alias the pool buffer. Field discipline
// matches vp9ReferenceFrame.storeWithRenderAndBitDepth minus the copies.
func (e *VP9Encoder) bindVP9EncoderRefView(slot int, src Image) {
	f := &e.refFrames[slot]
	f.img = src
	f.renderWidth = src.Width
	f.renderHeight = src.Height
	f.bitDepth = int(vp9dec.Bits8)
	f.external = nil
	f.internal = nil
	f.valid = true
}

// releaseVP9EncoderRefPoolSlot drops the pool reference held by a map slot
// (the "--bufs[ref_index].ref_count" half of ref_cnt_fb) when the slot is
// invalidated outside a refresh, e.g. on a resolution change.
func (e *VP9Encoder) releaseVP9EncoderRefPoolSlot(slot int) {
	pool := e.reconPool
	if pool == nil || slot < 0 || slot >= len(e.refPoolIdx) {
		return
	}
	old := e.refPoolIdx[slot]
	if old >= 0 && pool.refCount[old] > 0 {
		pool.refCount[old]--
	}
	e.refPoolIdx[slot] = -1
}

// storeVP9EncoderRefCopy copies externally supplied reference pixels into a
// free pool buffer and points the slot at it. This is the API-boundary copy
// for SetReferenceFrame and the spatial-SVC inter-layer reference seed: the
// pixels enter the pool once, and subsequent frames alias them like any
// other reference. The plane geometry uses the encoder's own FrameLayout
// (identical to the reconstruction buffers) with 128-filled padding, so any
// incidental read outside the visible region sees the same deterministic
// bytes as a freshly prepared reconstruction buffer.
//
// Returns false when the pool is unavailable; callers fall back to the
// legacy per-slot store.
func (e *VP9Encoder) storeVP9EncoderRefCopy(slot int, src Image) bool {
	pool := e.reconPool
	if pool == nil || slot < 0 || slot >= len(e.refPoolIdx) ||
		src.Width <= 0 || src.Height <= 0 {
		return false
	}
	idx := pool.freeIndex()
	if idx < 0 {
		return false
	}
	ent := &pool.frames[idx]
	layout := common.NewFrameLayout(src.Width, src.Height)
	ent.yFull = buffers.EnsureAlignedCapacity(ent.yFull, layout.YFullLen, 32)
	ent.uFull = buffers.EnsureAlignedCapacity(ent.uFull, layout.UVFullLen, 32)
	ent.vFull = buffers.EnsureAlignedCapacity(ent.vFull, layout.UVFullLen, 32)
	buffers.Fill(ent.yFull, 128)
	buffers.Fill(ent.uFull, 128)
	buffers.Fill(ent.vFull, 128)
	y := ent.yFull[layout.YOrigin:]
	u := ent.uFull[layout.UVOrigin:]
	v := ent.vFull[layout.UVOrigin:]
	uvW := (src.Width + 1) >> 1
	uvH := (src.Height + 1) >> 1
	buffers.CopyPlane(y, layout.YStride, src.Y, src.YStride, src.Width, src.Height)
	buffers.CopyPlane(u, layout.UVStride, src.U, src.UStride, uvW, uvH)
	buffers.CopyPlane(v, layout.UVStride, src.V, src.VStride, uvW, uvH)
	ent.img = Image{
		Width:   src.Width,
		Height:  src.Height,
		Y:       y,
		U:       u,
		V:       v,
		YStride: layout.YStride,
		UStride: layout.UVStride,
		VStride: layout.UVStride,
	}
	e.releaseVP9EncoderRefPoolSlot(slot)
	e.refPoolIdx[slot] = idx
	pool.refCount[idx]++
	e.bindVP9EncoderRefView(slot, ent.img)
	return true
}

// dropVP9EncoderFramePool detaches a cloned worker encoder from the parent's
// buffer pool. Worker clones must never rotate or refresh pool state: the
// pool and its reference counts belong to the owning encode goroutine.
func (e *VP9Encoder) dropVP9EncoderFramePool() {
	e.reconPool = nil
	e.reconPoolIdx = -1
}
