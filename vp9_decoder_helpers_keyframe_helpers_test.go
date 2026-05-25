package govpx

import "testing"

func vp9TopRightResidueKeyframeForNewMvTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 64, 64)
}

func vp9InteriorResidueKeyframeForSubpelTest(t *testing.T) []byte {
	t.Helper()
	return vp9ColumnResidueKeyframeForMotionTest(t, 96, 96)
}
