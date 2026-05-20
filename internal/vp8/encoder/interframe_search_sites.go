package encoder

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c full-pixel search-site
// tables.

// InterFrameNstepSearchSites and InterFrameDiamondSearchSites are
// pre-computed full-pel search-site arrays reused by VP8 inter motion search.
var InterFrameNstepSearchSites = buildInterFrameNstepSearchSites()
var InterFrameDiamondSearchSites = buildInterFrameDiamondSearchSites()

func buildInterFrameNstepSearchSites() [1 + InterFrameMaxMVSearchSteps*8]MotionVector {
	var sites [1 + InterFrameMaxMVSearchSteps*8]MotionVector
	count := 1
	for length := 1 << (InterFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(length)}
		count++
		sites[count] = MotionVector{Row: int16(-length), Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: int16(-length), Col: int16(length)}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: int16(length)}
		count++
	}
	return sites
}

func buildInterFrameDiamondSearchSites() [1 + InterFrameMaxMVSearchSteps*4]MotionVector {
	var sites [1 + InterFrameMaxMVSearchSteps*4]MotionVector
	count := 1
	for length := 1 << (InterFrameMaxMVSearchSteps - 1); length > 0; length /= 2 {
		sites[count] = MotionVector{Row: int16(-length), Col: 0}
		count++
		sites[count] = MotionVector{Row: int16(length), Col: 0}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(-length)}
		count++
		sites[count] = MotionVector{Row: 0, Col: int16(length)}
		count++
	}
	return sites
}
