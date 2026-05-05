package libgopx

import vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"

type StreamInfo struct {
	Width   int
	Height  int
	Profile int

	KeyFrame  bool
	ShowFrame bool

	FirstPartitionSize int
}

type FrameInfo struct {
	Width  int
	Height int

	KeyFrame  bool
	ShowFrame bool
	Corrupted bool

	PTS uint64
}

func PeekVP8StreamInfo(packet []byte) (StreamInfo, error) {
	header, err := vp8dec.ParseFrameHeader(packet)
	if err != nil {
		return StreamInfo{}, ErrInvalidData
	}
	return StreamInfo{
		Width:              header.Width,
		Height:             header.Height,
		Profile:            header.Profile,
		KeyFrame:           header.KeyFrame(),
		ShowFrame:          header.ShowFrame,
		FirstPartitionSize: header.FirstPartitionSize,
	}, nil
}
