//go:build govpx_oracle_trace

package govpx

import "github.com/thesyncim/govpx/internal/testutil/vp8corpus"

func vp8SourceClipImages(clip vp8corpus.SourceClip) []Image {
	frames := make([]Image, len(clip.Frames))
	for i, frame := range clip.Frames {
		frames[i] = Image{
			Width:   clip.Width,
			Height:  clip.Height,
			Y:       frame.Y,
			U:       frame.Cb,
			V:       frame.Cr,
			YStride: frame.YStride,
			UStride: frame.CStride,
			VStride: frame.CStride,
		}
	}
	return frames
}
