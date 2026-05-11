package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"

	govpx "github.com/thesyncim/govpx"
)

func makeBenchmarkFrame(width int, height int, index int) govpx.Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	img := govpx.Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for row := range height {
		for col := range width {
			img.Y[row*img.YStride+col] = byte(32 + ((row*3 + col*5 + index*7) & 191))
		}
	}
	for row := range uvHeight {
		for col := range uvWidth {
			img.U[row*img.UStride+col] = byte(96 + ((row*2 + col + index*3) & 63))
			img.V[row*img.VStride+col] = byte(144 + ((row + col*2 + index*5) & 63))
		}
	}
	return img
}
func makeBenchmarkIVF(width int, height int, fps int, packets [][]byte) []byte {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	size := fileHeaderSize
	for _, packet := range packets {
		size += frameHeaderSize + len(packet)
	}
	ivf := make([]byte, size)
	copy(ivf[:4], []byte("DKIF"))
	binary.LittleEndian.PutUint16(ivf[4:], 0)
	binary.LittleEndian.PutUint16(ivf[6:], fileHeaderSize)
	copy(ivf[8:12], []byte("VP80"))
	binary.LittleEndian.PutUint16(ivf[12:], uint16(width))
	binary.LittleEndian.PutUint16(ivf[14:], uint16(height))
	binary.LittleEndian.PutUint32(ivf[16:], uint32(fps))
	binary.LittleEndian.PutUint32(ivf[20:], 1)
	binary.LittleEndian.PutUint32(ivf[24:], uint32(len(packets)))
	offset := fileHeaderSize
	for i, packet := range packets {
		binary.LittleEndian.PutUint32(ivf[offset:], uint32(len(packet)))
		binary.LittleEndian.PutUint64(ivf[offset+4:], uint64(i))
		offset += frameHeaderSize
		copy(ivf[offset:], packet)
		offset += len(packet)
	}
	return ivf
}

func referenceQualityMetrics(ivf []byte, frames []govpx.Image) (float64, float64, int, error) {
	dec, err := govpx.NewVP8Decoder(govpx.DecoderOptions{})
	if err != nil {
		return 0, 0, 0, err
	}
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		return 0, 0, 0, errors.New("invalid IVF header")
	}
	offset := fileHeaderSize
	psnrSum := 0.0
	ssimSum := 0.0
	qualityFrames := 0
	for frameIndex := 0; offset < len(ivf); frameIndex++ {
		if offset+frameHeaderSize > len(ivf) {
			return 0, 0, qualityFrames, errors.New("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		timestamp := binary.LittleEndian.Uint64(ivf[offset+4:])
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			return 0, 0, qualityFrames, errors.New("truncated IVF frame payload")
		}
		packet := ivf[offset : offset+size]
		offset += size
		if err := dec.Decode(packet); err != nil {
			return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, fmt.Errorf("decode reference frame %d: %w", frameIndex, err))
		}
		decoded, ok := dec.NextFrame()
		if !ok {
			continue
		}
		sourceIndex := frameIndex
		if timestamp < uint64(len(frames)) {
			sourceIndex = int(timestamp)
		}
		if sourceIndex >= len(frames) {
			continue
		}
		source := frames[sourceIndex]
		psnrSum += imagePSNR(source, decoded)
		ssimSum += imageSSIM(source, decoded)
		qualityFrames++
	}
	return averageReferenceQuality(psnrSum, ssimSum, qualityFrames, nil)
}

func averageReferenceQuality(psnrSum float64, ssimSum float64, count int, err error) (float64, float64, int, error) {
	if count == 0 {
		return 0, 0, 0, err
	}
	return psnrSum / float64(count), ssimSum / float64(count), count, err
}

func writeI420Frame(dst *os.File, frame govpx.Image) error {
	if err := writePlane(dst, frame.Y, frame.YStride, frame.Width, frame.Height); err != nil {
		return err
	}
	uvWidth := (frame.Width + 1) >> 1
	uvHeight := (frame.Height + 1) >> 1
	if err := writePlane(dst, frame.U, frame.UStride, uvWidth, uvHeight); err != nil {
		return err
	}
	return writePlane(dst, frame.V, frame.VStride, uvWidth, uvHeight)
}

func writePlane(dst *os.File, plane []byte, stride int, width int, height int) error {
	for row := range height {
		if _, err := dst.Write(plane[row*stride : row*stride+width]); err != nil {
			return err
		}
	}
	return nil
}

func parseIVFFrameSizes(ivf []byte) ([]int, error) {
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(ivf) < fileHeaderSize || string(ivf[:4]) != "DKIF" {
		return nil, errors.New("invalid IVF header")
	}
	offset := fileHeaderSize
	var sizes []int
	for offset < len(ivf) {
		if offset+frameHeaderSize > len(ivf) {
			return nil, errors.New("truncated IVF frame header")
		}
		size := int(binary.LittleEndian.Uint32(ivf[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(ivf) {
			return nil, errors.New("truncated IVF frame payload")
		}
		sizes = append(sizes, size)
		offset += size
	}
	return sizes, nil
}

func imagePSNR(src govpx.Image, dst govpx.Image) float64 {
	sse, count := planeSSE(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSE, uCount := planeSSE(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSE, vCount := planeSSE(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	sse += uSSE + vSSE
	count += uCount + vCount
	if sse == 0 {
		return 100
	}
	mse := float64(sse) / float64(count)
	return 10 * math.Log10((255*255)/mse)
}

func imageSSIM(src govpx.Image, dst govpx.Image) float64 {
	ssim, count := planeSSIM(src.Y, src.YStride, dst.Y, dst.YStride, src.Width, src.Height)
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	uSSIM, uCount := planeSSIM(src.U, src.UStride, dst.U, dst.UStride, uvWidth, uvHeight)
	vSSIM, vCount := planeSSIM(src.V, src.VStride, dst.V, dst.VStride, uvWidth, uvHeight)
	total := count + uCount + vCount
	if total == 0 {
		return 0
	}
	return (ssim*float64(count) + uSSIM*float64(uCount) + vSSIM*float64(vCount)) / float64(total)
}

func planeSSIM(a []byte, aStride int, b []byte, bStride int, width int, height int) (float64, int) {
	count := width * height
	if count <= 0 {
		return 0, 0
	}
	sumA := 0.0
	sumB := 0.0
	sumAA := 0.0
	sumBB := 0.0
	sumAB := 0.0
	for row := range height {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := range width {
			x := float64(aRow[col])
			y := float64(bRow[col])
			sumA += x
			sumB += y
			sumAA += x * x
			sumBB += y * y
			sumAB += x * y
		}
	}
	n := float64(count)
	meanA := sumA / n
	meanB := sumB / n
	varA := sumAA/n - meanA*meanA
	varB := sumBB/n - meanB*meanB
	cov := sumAB/n - meanA*meanB
	const (
		c1 = 6.5025
		c2 = 58.5225
	)
	num := (2*meanA*meanB + c1) * (2*cov + c2)
	den := (meanA*meanA + meanB*meanB + c1) * (varA + varB + c2)
	if den == 0 {
		return 1, count
	}
	return num / den, count
}

func planeSSE(a []byte, aStride int, b []byte, bStride int, width int, height int) (uint64, int) {
	var sse uint64
	for row := range height {
		aRow := a[row*aStride:]
		bRow := b[row*bStride:]
		for col := range width {
			diff := int(aRow[col]) - int(bRow[col])
			sse += uint64(diff * diff)
		}
	}
	return sse, width * height
}

func percentileLatency(latencies []int64, percentile int) int64 {
	if len(latencies) == 0 {
		return 0
	}
	sorted := append([]int64(nil), latencies...)
	slices.Sort(sorted)
	index := (len(sorted)*percentile + 99) / 100
	if index <= 0 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}
