package scale

import "testing"

// TestScaleFrame_OneTwo_DownsamplesEachPlane verifies the YV12 wrapper
// invokes Scale2D for Y/U/V at the 1:2 ratio and produces non-zero
// outputs across all three planes. Per-pixel correctness of the 1:2
// interpolated path lives in Scale2D's own tests (TestScale2D_*); this
// test covers the YV12 plane wiring only.
func TestScaleFrame_OneTwo_DownsamplesEachPlane(t *testing.T) {
	const srcYW, srcYH = 16, 16
	const dstYW, dstYH = 8, 8

	src := &Frame{
		Y:        ramp(srcYW * srcYH),
		U:        ramp(srcYW * srcYH / 4),
		V:        ramp(srcYW * srcYH / 4),
		YStride:  srcYW,
		UVStride: srcYW / 2,
		YWidth:   srcYW,
		YHeight:  srcYH,
	}
	dst := &Frame{
		Y:        make([]byte, dstYW*dstYH),
		U:        make([]byte, dstYW*dstYH/4),
		V:        make([]byte, dstYW*dstYH/4),
		YStride:  dstYW,
		UVStride: dstYW / 2,
		YWidth:   dstYW,
		YHeight:  dstYH,
	}
	temp := make([]byte, 6*dstYW)
	hr, hs := Scale2Ratio(ModeOneTwo)
	ScaleFrame(src, dst, temp, 6, hs, hr, hs, hr, false)

	for name, plane := range map[string][]byte{"Y": dst.Y, "U": dst.U, "V": dst.V} {
		allZero := true
		for _, b := range plane {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatalf("dst.%s is all zero, scale never ran on this plane", name)
		}
	}
}

// TestScaleFrame_FourFive_ProducesScaledDims verifies the 4:5 mode
// produces dimensions matching ScaledDimension and writes through to
// chroma planes.
func TestScaleFrame_FourFive_ProducesScaledDims(t *testing.T) {
	const srcYW, srcYH = 20, 20
	dstW := ScaledDimension(srcYW, ModeFourFive)
	dstH := ScaledDimension(srcYH, ModeFourFive)
	if dstW != 16 || dstH != 16 {
		t.Fatalf("ScaledDimension(20, 4:5) = (%d, %d), want (16, 16)", dstW, dstH)
	}

	src := &Frame{
		Y:        ramp(srcYW * srcYH),
		U:        ramp(srcYW * srcYH / 4),
		V:        ramp(srcYW * srcYH / 4),
		YStride:  srcYW,
		UVStride: srcYW / 2,
		YWidth:   srcYW,
		YHeight:  srcYH,
	}
	dst := &Frame{
		Y:        make([]byte, dstW*dstH),
		U:        make([]byte, (dstW/2)*(dstH/2)),
		V:        make([]byte, (dstW/2)*(dstH/2)),
		YStride:  dstW,
		UVStride: dstW / 2,
		YWidth:   dstW,
		YHeight:  dstH,
	}
	temp := make([]byte, 6*dstW)
	hr, hs := Scale2Ratio(ModeFourFive)
	ScaleFrame(src, dst, temp, 6, hs, hr, hs, hr, false)

	for name, plane := range map[string][]byte{"Y": dst.Y, "U": dst.U, "V": dst.V} {
		allZero := true
		for _, b := range plane {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Fatalf("dst.%s is all zero, scale never ran on this plane", name)
		}
	}
}

func ramp(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i & 0xff)
	}
	return out
}
