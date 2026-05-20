package govpx

import "github.com/thesyncim/govpx/internal/testutil"

func ivfFramesForThreadingParity(ivf []byte) ([][]byte, error) {
	offset, err := testutil.FirstIVFFrameOffset(ivf)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for offset < len(ivf) {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, len(frames))
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame.Data)
		offset = next
	}
	return frames, nil
}
