package govpx

import (
	"image"
	"math"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// VP9TPLFrameDelta is the read-only per-SB TPL summary exposed to other
// passes (row-MT, oracle traces). The Delta map exposes the per-SB rdmult
// scaler as a fixed-point ratio so downstream consumers can apply the same
// scaling libvpx does without depending on the TPL pass internals.
type VP9TPLFrameDelta struct {
	SBRows int
	SBCols int
	// Delta is a per-SB int8 mapping where 0 means "no scaling". The
	// encoded value is clamp(round((beta-1)*16), -128, 127); the
	// keyframe mode picker applies it as rdmult * (1 + value/16).
	Delta []int8
}

// vp9TPLEnabled reports whether the encoder has the TPL pass active.
func (e *VP9Encoder) vp9TPLEnabled() bool {
	if e == nil || !e.opts.EnableTPL {
		return false
	}
	return e.tpl.Enabled
}

// TPLFrameDelta returns the per-SB TPL summary for the next frame to be
// encoded. The returned slice is read-only and is allocated lazily on each
// call; mutating it is a misuse. When TPL is disabled, no slab has been
// populated yet, or the resolution has changed since the last populate call,
// Delta is nil and SBRows/SBCols are zero.
func (e *VP9Encoder) TPLFrameDelta() VP9TPLFrameDelta {
	if e == nil || e.closed {
		return VP9TPLFrameDelta{}
	}
	slab := e.tpl.FrameSlab()
	if slab == nil {
		return VP9TPLFrameDelta{}
	}
	r0 := slab.R0
	delta := make([]int8, len(slab.Stats))
	for i := range slab.Stats {
		st := &slab.Stats[i]
		if st.McDepCost <= 0 || st.IntraCost <= 0 || r0 <= 0 {
			continue
		}
		// rk = intra_cost / mc_dep_cost; beta = r0 / rk.
		rk := float64(st.IntraCost) / float64(st.McDepCost)
		if rk <= 0 {
			continue
		}
		beta := r0 / rk
		scaled := math.Round((beta - 1) * 16)
		switch {
		case scaled > 127:
			delta[i] = 127
		case scaled < -128:
			delta[i] = -128
		default:
			delta[i] = int8(scaled)
		}
	}
	return VP9TPLFrameDelta{
		SBRows: slab.SBRows,
		SBCols: slab.SBCols,
		Delta:  delta,
	}
}

// SetEnableTPL toggles the VP9 TPL quality pass at runtime. Enabling requires
// the encoder to have been constructed with LookaheadFrames >= 8 and
// AutoAltRef enabled, so the lookahead window stays populated for the future
// frames TPL inspects.
func (e *VP9Encoder) SetEnableTPL(enabled bool) error {
	if e == nil || e.closed {
		return ErrClosed
	}
	if enabled {
		opts := e.opts
		opts.EnableTPL = true
		if err := validateVP9TPLOptions(opts); err != nil {
			return err
		}
	}
	e.opts.EnableTPL = enabled
	e.tpl.Configure(enabled, e.opts.Width, e.opts.Height,
		e.opts.LookaheadFrames)
	return nil
}

// populateVP9TPLForFrame collects the lookahead sources visible from the
// current frame into a contiguous slice and asks the TPL pass to refresh its
// per-frame slabs. The skip parameter mirrors the libvpx gate: TPL is
// inactive on hidden and alt-ref frames because the pass needs a source-order
// future to inspect.
func (e *VP9Encoder) populateVP9TPLForFrame(skip bool, current *image.YCbCr) {
	if !e.vp9TPLEnabled() {
		return
	}
	if skip {
		e.tpl.InvalidateAll()
		return
	}
	if !e.vp9LookaheadEnabled() {
		e.tpl.InvalidateAll()
		return
	}
	tail := e.collectVP9TPLLookaheadFrames()
	var frames []*image.YCbCr
	if current != nil {
		frames = make([]*image.YCbCr, 0, 1+len(tail))
		frames = append(frames, current)
		frames = append(frames, tail...)
	} else {
		frames = tail
	}
	if len(frames) < encoder.TPLMinLookaheadFrames {
		// libvpx computes the TPL plan once per GOP and serves it while the
		// GOP drains. Once the lookahead has drained mid-GOP, keep the
		// shifted slabs rather than invalidating the active rdmult delta.
		return
	}
	e.tpl.Populate(frames)
}

// collectVP9TPLLookaheadFrames returns pointers to the lookahead source
// images in source order, starting at the next-to-encode frame. The returned
// slice aliases the lookahead ring buffer and remains valid only for the
// duration of populateVP9TPLForFrame.
func (e *VP9Encoder) collectVP9TPLLookaheadFrames() []*image.YCbCr {
	if !e.vp9LookaheadEnabled() {
		return nil
	}
	count := int(e.lookaheadCount)
	if count == 0 {
		return nil
	}
	out := make([]*image.YCbCr, 0, count)
	idx := int(e.lookaheadRead)
	for range count {
		out = append(out, &e.lookahead[idx].img)
		idx++
		if idx >= len(e.lookahead) {
			idx = 0
		}
	}
	return out
}

// getVP9TPLRDMultDelta applies the internal TPL rdmult lookup and keeps the
// root-owned test counter out of the internal encoder package.
func (e *VP9Encoder) getVP9TPLRDMultDelta(miRow, miCol, blockMiHigh, blockMiWide,
	origRdmult int) int {
	if origRdmult <= 0 {
		return 1
	}
	if e == nil || !e.opts.EnableTPL || !e.tpl.Enabled {
		return origRdmult
	}
	return e.tpl.RDMultDelta(miRow, miCol, blockMiHigh, blockMiWide,
		origRdmult)
}

// validateVP9TPLOptions enforces the libvpx-compatible TPL prerequisites.
func validateVP9TPLOptions(opts VP9EncoderOptions) error {
	if !opts.EnableTPL {
		return nil
	}
	if opts.LookaheadFrames < encoder.TPLMinLookaheadFrames {
		return ErrInvalidConfig
	}
	if !opts.AutoAltRef {
		return ErrInvalidConfig
	}
	if opts.Lossless {
		return ErrInvalidConfig
	}
	return nil
}
