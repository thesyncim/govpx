package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

type interAnalysisReference struct {
	Frame      vp8common.MVReferenceFrame
	Img        *vp8common.Image
	RefRate    int
	RefRateSet bool
}

type interMacroblockImageSnapshot struct {
	y [16 * 16]byte
	u [8 * 8]byte
	v [8 * 8]byte
}

func snapshotInterMacroblockImage(img *vp8common.Image, row int, col int, snap *interMacroblockImageSnapshot) {
	if img == nil || snap == nil {
		return
	}
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	copyMacroblockY(snap.y[:], 16, img.Y[yOff:], img.YStride)
	copyMacroblock8x8(snap.u[:], 8, img.U[uOff:], img.UStride)
	copyMacroblock8x8(snap.v[:], 8, img.V[vOff:], img.VStride)
}

func restoreInterMacroblockImage(img *vp8common.Image, row int, col int, snap *interMacroblockImageSnapshot) {
	if img == nil || snap == nil {
		return
	}
	yOff := row*16*img.YStride + col*16
	uOff := row*8*img.UStride + col*8
	vOff := row*8*img.VStride + col*8
	copyMacroblockY(img.Y[yOff:], img.YStride, snap.y[:], 16)
	copyMacroblock8x8(img.U[uOff:], img.UStride, snap.u[:], 8)
	copyMacroblock8x8(img.V[vOff:], img.VStride, snap.v[:], 8)
}

type interAnalysisMotionCandidate struct {
	Ref interAnalysisReference
	MV  vp8enc.MotionVector
}

func (e *VP8Encoder) interAnalysisReferences(flags EncodeFlags, refs *[3]interAnalysisReference) int {
	count := 0
	lastRate, goldenRate, altRate := e.interReferenceFrameRatesForFlags(flags)
	lastEnabled, goldenEnabled, altEnabled := e.interReferenceAvailability(flags)
	if lastEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.LastFrame, Img: &e.lastRef.Img, RefRate: lastRate, RefRateSet: true}
		count++
	}
	if goldenEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.GoldenFrame, Img: &e.goldenRef.Img, RefRate: goldenRate, RefRateSet: true}
		count++
	}
	if altEnabled {
		refs[count] = interAnalysisReference{Frame: vp8common.AltRefFrame, Img: &e.altRef.Img, RefRate: altRate, RefRateSet: true}
		count++
	}
	return count
}

func (e *VP8Encoder) closestInterAnalysisReference(refs []interAnalysisReference, refCount int) vp8common.MVReferenceFrame {
	closest := vp8common.IntraFrame
	limit := min(refCount, len(refs))
	for i := range limit {
		refFrame := refs[i].Frame
		if refFrame < vp8common.LastFrame || refFrame >= vp8common.MaxRefFrames {
			continue
		}
		if closest == vp8common.IntraFrame || e.referenceFrameNumbers[refFrame] > e.referenceFrameNumbers[closest] {
			closest = refFrame
		}
	}
	if closest == vp8common.IntraFrame {
		return vp8common.LastFrame
	}
	return closest
}

func interAnalysisReferencesInclude(refs []interAnalysisReference, refCount int, frame vp8common.MVReferenceFrame) bool {
	limit := min(refCount, len(refs))
	for i := range limit {
		if refs[i].Frame == frame {
			return true
		}
	}
	return false
}

func interAnalysisValidReferenceCount(refs []interAnalysisReference, refCount int) int {
	limit := min(refCount, len(refs))
	count := 0
	for i := range limit {
		if refs[i].Img != nil && refs[i].Frame >= vp8common.LastFrame && refs[i].Frame < vp8common.MaxRefFrames {
			count++
		}
	}
	return count
}

func (e *VP8Encoder) interAnalysisMacroblockCount() int {
	if e.opts.Width > 0 && e.opts.Height > 0 {
		return encoderMacroblockCount(e.opts.Width, e.opts.Height)
	}
	return len(e.interFrameModes)
}

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c motion search.
// vp8_hex_search finishes with an eight-step full-pixel diamond refinement.
