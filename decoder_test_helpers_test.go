package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

func vp8KeyFramePacketWithFirstPartition(width int, height int, first []byte) []byte {
	return vp8KeyFramePacketWithFirstPartitionProfile(width, height, 0, first)
}

func vp8KeyFramePacketWithFirstPartitionProfile(width int, height int, profile int, first []byte) []byte {
	packet := vp8KeyFramePacket(width, height, len(first), profile, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

func vp8InterFramePacketWithFirstPartition(first []byte) []byte {
	packet := vp8InterFramePacket(len(first), 0, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

func vp8InterFramePacketWithTokenPartitions(first []byte, firstTokenSize int, tokens []byte) []byte {
	packet := vp8InterFramePacket(len(first), 0, true)
	packet = append(packet, first...)
	packet = append(packet, byte(firstTokenSize), byte(firstTokenSize>>8), byte(firstTokenSize>>16))
	return append(packet, tokens...)
}

func vp8InterFirstPartitionLastZeroMV() []byte {
	return vp8InterFirstPartitionLastZeroMVWithTokenPartition(vp8common.OnePartition, false)
}

func vp8InterFirstPartitionLastZeroMVWithTokenPartition(tokenPartition vp8common.TokenPartition, skipCoeff bool) []byte {
	return vp8InterFirstPartitionLastZeroMVWithConfig(tokenPartition, skipCoeff, 0)
}

func vp8InterFirstPartitionLastZeroMVWithConfig(tokenPartition vp8common.TokenPartition, skipCoeff bool, baseQIndex uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(tokenPartition), 2)
	w.writeLiteral(uint32(baseQIndex&0x7f), 7)
	for range 5 {
		w.writeBool(0, 128)
	}

	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)

	writeNoCoefficientProbabilityUpdates(&w)
	if skipCoeff {
		w.writeBool(1, 128)
		w.writeLiteral(128, 8)
	} else {
		w.writeBool(0, 128)
	}
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	for component := range 2 {
		for i := range vp8tables.MVPCount {
			w.writeBool(0, vp8tables.MVUpdateProbs[component][i])
		}
	}

	if skipCoeff {
		w.writeBool(1, 128)
	}
	w.writeBool(1, 128)
	w.writeBool(0, 128)
	w.writeBool(0, vp8tables.InterModeContexts[0][0])

	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithBaseQIndex(baseQIndex uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(uint32(baseQIndex&0x7f), 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithSingleCoefProbabilityUpdate(refreshEntropy bool, value uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	if refreshEntropy {
		w.writeBool(1, 128)
	} else {
		w.writeBool(0, 128)
	}

	first := true
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					if first {
						w.writeBool(1, vp8tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}

	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithLoopFilterLevel(level uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(level), 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func vp8FirstPartitionWithMacroblockSkip(probSkipFalse uint8) []byte {
	var w vp8TestBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	writeNoCoefficientProbabilityUpdates(&w)
	w.writeBool(1, 128)
	w.writeLiteral(uint32(probSkipFalse), 8)
	w.writeBool(1, probSkipFalse)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func writeNoCoefficientProbabilityUpdates(w *vp8TestBoolWriter) {
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
				}
			}
		}
	}
}

type vp8TestBoolWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *vp8TestBoolWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *vp8TestBoolWriter) writeLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.writeBool(uint8((value>>uint(bit))&1), 128)
	}
}

func (w *vp8TestBoolWriter) finish() []byte {
	for range 32 {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *vp8TestBoolWriter) writeBool(bit uint8, probability uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))

	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(vp8tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}

		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func assertCodedBordersExtended(t *testing.T, img *vp8common.Image) {
	t.Helper()

	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1

	yRightEdge := img.Y[img.CodedWidth-1]
	if got := img.Y[img.CodedWidth]; got != yRightEdge {
		t.Fatalf("first Y right border = %d, want coded edge %d", got, yRightEdge)
	}
	yBottomEdge := img.Y[(img.CodedHeight-1)*img.YStride+img.CodedWidth-1]
	if got := img.YFull[img.YOrigin+img.CodedHeight*img.YStride+img.CodedWidth-1]; got != yBottomEdge {
		t.Fatalf("first Y bottom border = %d, want coded edge %d", got, yBottomEdge)
	}

	uRightEdge := img.U[codedUVWidth-1]
	if got := img.U[codedUVWidth]; got != uRightEdge {
		t.Fatalf("first U right border = %d, want coded edge %d", got, uRightEdge)
	}
	uBottomEdge := img.U[(codedUVHeight-1)*img.UStride+codedUVWidth-1]
	if got := img.UFull[img.UOrigin+codedUVHeight*img.UStride+codedUVWidth-1]; got != uBottomEdge {
		t.Fatalf("first U bottom border = %d, want coded edge %d", got, uBottomEdge)
	}

	vRightEdge := img.V[codedUVWidth-1]
	if got := img.V[codedUVWidth]; got != vRightEdge {
		t.Fatalf("first V right border = %d, want coded edge %d", got, vRightEdge)
	}
	vBottomEdge := img.V[(codedUVHeight-1)*img.VStride+codedUVWidth-1]
	if got := img.VFull[img.VOrigin+codedUVHeight*img.VStride+codedUVWidth-1]; got != vBottomEdge {
		t.Fatalf("first V bottom border = %d, want coded edge %d", got, vBottomEdge)
	}
}

func fillVP8Image(img *vp8common.Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func newTestImage(width int, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func publicImageEqualVP8(got Image, want *vp8common.Image) bool {
	if want == nil || got.Width != want.Width || got.Height != want.Height {
		return false
	}
	uvWidth := (want.Width + 1) >> 1
	uvHeight := (want.Height + 1) >> 1
	return planeEqual(got.Y, got.YStride, want.Y, want.YStride, want.Width, want.Height) &&
		planeEqual(got.U, got.UStride, want.U, want.UStride, uvWidth, uvHeight) &&
		planeEqual(got.V, got.VStride, want.V, want.VStride, uvWidth, uvHeight)
}

func planeEqual(a []byte, aStride int, b []byte, bStride int, width int, height int) bool {
	for row := range height {
		for col := range width {
			if a[row*aStride+col] != b[row*bStride+col] {
				return false
			}
		}
	}
	return true
}
