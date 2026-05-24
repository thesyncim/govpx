package testutil

import "github.com/thesyncim/govpx/internal/vpx/ivf"

const (
	IVFFileHeaderSize  = ivf.FileHeaderSize
	IVFFrameHeaderSize = ivf.FrameHeaderSize
)

var (
	// IVFFourCCVP8 is the IVF stream fourcc used by VP8 payloads.
	IVFFourCCVP8 = ivf.FourCCVP8
	// IVFFourCCVP9 is the IVF stream fourcc used by VP9 payloads.
	IVFFourCCVP9 = ivf.FourCCVP9
)

var (
	ErrInvalidIVF        = ivf.ErrInvalid
	ErrUnsupportedFourCC = ivf.ErrUnsupportedFourCC
)

type IVFHeader = ivf.Header
type IVFFrame = ivf.Frame

func ParseIVFHeader(data []byte) (IVFHeader, error) {
	return ivf.ParseHeader(data)
}

func FirstIVFFrameOffset(data []byte) (int, error) {
	return ivf.FirstFrameOffset(data)
}

func NextIVFFrame(data []byte, offset int, index int) (IVFFrame, int, error) {
	return ivf.NextFrame(data, offset, index)
}

func CountIVFFrames(data []byte) (int, error) {
	return ivf.CountFrames(data)
}

func IVFFrames(data []byte) ([]IVFFrame, error) {
	return ivf.Frames(data)
}

func IVFFramePayloads(data []byte) ([][]byte, error) {
	return ivf.FramePayloads(data)
}

func IVFFramePayloadViews(data []byte) ([][]byte, error) {
	return ivf.FramePayloadViews(data)
}

func IVFFramePayloadSizeSummary(data []byte) (total int, frames int, err error) {
	return ivf.FramePayloadSizeSummary(data)
}

func WriteIVFHeader(h IVFHeader) []byte {
	return ivf.WriteHeader(h)
}

func WriteIVFFrame(payload []byte, pts uint64) []byte {
	return ivf.WriteFrame(payload, pts)
}

func BuildIVF(h IVFHeader, payloads [][]byte) []byte {
	return ivf.Build(h, payloads)
}

func BuildVP8IVF(width int, height int, den uint32, num uint32, payloads [][]byte) []byte {
	return ivf.BuildVP8(width, height, den, num, payloads)
}

func BuildSingleFrameVP8IVF(width int, height int, den uint32, num uint32, payload []byte) []byte {
	return ivf.BuildSingleFrameVP8(width, height, den, num, payload)
}
