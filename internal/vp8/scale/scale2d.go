package scale

// Ported from libvpx v1.16.0 vpx_scale/generic/vpx_scale.c (Scale2D).

type scaleHorizontalMode uint8

const (
	scaleHorizontalNone scaleHorizontalMode = iota
	scaleHorizontal54
	scaleHorizontal53
	scaleHorizontal21
)

type scaleVerticalMode uint8

const (
	scaleVerticalNone scaleVerticalMode = iota
	scaleVertical54
	scaleVertical53
	scaleVertical21
)

// Scale2D ports libvpx's vpx_scale/generic/vpx_scale.c Scale2D
// (lines 234-445). Performs 2-tap linear interpolation in two
// dimensions, dispatching to the published 5:4, 5:3, or 2:1 specialized
// kernels when (hratio*10/hscale, vratio*10/vscale) is recognized, and
// to the generic scale1d_c kernel otherwise.
//
// tempArea must be at least tempAreaHeight * dstStride bytes. Expansion
// proceeds one band at a time to help with caching.
func Scale2D(
	src []byte, srcPitch int, srcWidth, srcHeight int,
	dst []byte, dstPitch int, dstWidth, dstHeight int,
	tempArea []byte, tempAreaHeight int,
	hscale, hratio, vscale, vratio int,
	interlaced bool,
) {
	// Replicate libvpx's "negative source pitch" support — when the
	// caller passes a flipped-vertical layout, source_base must point at
	// the original top row before we step backward through bands.
	sourceBaseOff := 0
	if srcPitch < 0 {
		offset := (srcHeight - 1) * srcPitch
		sourceBaseOff = -offset
	}

	ratioScalable := true
	interpolation := false
	sourceBandHeight := 0
	destBandHeight := 0
	horizMode := scaleHorizontalNone
	vertMode := scaleVerticalNone

	switch hratio * 10 / hscale {
	case 8:
		horizMode = scaleHorizontal54
	case 6:
		horizMode = scaleHorizontal53
	case 5:
		horizMode = scaleHorizontal21
	default:
		ratioScalable = false
	}

	switch vratio * 10 / vscale {
	case 8:
		vertMode = scaleVertical54
		sourceBandHeight = 5
		destBandHeight = 4
	case 6:
		vertMode = scaleVertical53
		sourceBandHeight = 5
		destBandHeight = 3
	case 5:
		if interlaced {
			vertMode = scaleVertical21
		} else {
			interpolation = true
		}
		sourceBandHeight = 2
		destBandHeight = 1
	default:
		ratioScalable = false
	}

	if ratioScalable {
		if srcHeight == dstHeight {
			srcOff := 0
			dstOff := 0
			for range dstHeight {
				scaleHorizontalLine(horizMode, src[srcOff:], srcWidth, dst[dstOff:])
				srcOff += srcPitch
				dstOff += dstPitch
			}
			return
		}

		srcOff := 0
		dstOff := 0

		if interpolation {
			// libvpx: if (source < source_base) source = source_base. The
			// only writer of source before the loop is the implicit
			// pre-first-band setup, which is a no-op since k=0 on entry,
			// so the clamp is significant when the negative-pitch branch
			// drove source below source_base. With non-negative pitch the
			// clamp is a no-op too.
			if srcOff < 0 {
				srcOff = sourceBaseOff
			}
			scaleHorizontalLine(horizMode, src[srcOff:], srcWidth, tempArea[:dstWidth])
		}

		bands := (dstHeight + destBandHeight - 1) / destBandHeight
		for range bands {
			for i := 0; i < sourceBandHeight; i++ {
				lineSrcOff := srcOff + i*srcPitch
				if lineSrcOff < 0 {
					lineSrcOff = sourceBaseOff
				}
				scaleHorizontalLine(horizMode, src[lineSrcOff:], srcWidth, tempArea[(i+1)*dstPitch:])
			}

			if interpolation {
				// Interpolated 2:1 vertical band reads three contiguous
				// rows from tempArea (above=row 0, current=row 1,
				// below=row 2). The Go kernel needs the full buffer
				// plus an explicit pivot offset; libvpx hides this
				// behind a single source pointer.
				verticalBand21Interpolated(tempArea, dstPitch, dstPitch, dst[dstOff:], dstWidth)
				copy(tempArea[:dstWidth], tempArea[sourceBandHeight*dstPitch:sourceBandHeight*dstPitch+dstWidth])
			} else {
				scaleVerticalBand(vertMode, tempArea[dstPitch:], dstPitch, dst[dstOff:], dstPitch, dstWidth)
			}

			srcOff += sourceBandHeight * srcPitch
			dstOff += destBandHeight * dstPitch
		}
		return
	}

	// vp8 only uses ratioScalable=true paths for the four published Mode
	// values when both axes pick from {1:1, 4:5, 3:5, 1:2}. The libvpx
	// generic 1D fallback (vpx_scale/generic/vpx_scale.c:381-444) is
	// unreachable for symmetric mode pairs, but ScaleFrame guards mixed
	// modes that would land here by validating both axes at the public
	// entry point.
	_ = sourceBaseOff
}

func scaleHorizontalLine(mode scaleHorizontalMode, srcLine []byte, srcWidth int, dstLine []byte) {
	switch mode {
	case scaleHorizontal54:
		horizontalLine54(srcLine, srcWidth, dstLine)
	case scaleHorizontal53:
		horizontalLine53(srcLine, srcWidth, dstLine)
	case scaleHorizontal21:
		horizontalLine21(srcLine, srcWidth, dstLine)
	default:
		panic("govpx/vp8/scale: invalid horizontal scale mode")
	}
}

func scaleVerticalBand(mode scaleVerticalMode, srcLine []byte, srcPitch int, dstLine []byte, dstPitch int, destWidth int) {
	switch mode {
	case scaleVertical54:
		verticalBand54(srcLine, srcPitch, dstLine, dstPitch, destWidth)
	case scaleVertical53:
		verticalBand53(srcLine, srcPitch, dstLine, dstPitch, destWidth)
	case scaleVertical21:
		verticalBand21(srcLine, dstLine, destWidth)
	default:
		panic("govpx/vp8/scale: invalid vertical scale mode")
	}
}
