package testutil

type FrameChecksum struct {
	Index int

	Width  int
	Height int

	KeyFrame  bool
	ShowFrame bool

	MD5 PlaneMD5
}

func SameFrameChecksum(a FrameChecksum, b FrameChecksum) bool {
	return a.Index == b.Index &&
		a.Width == b.Width &&
		a.Height == b.Height &&
		a.KeyFrame == b.KeyFrame &&
		a.ShowFrame == b.ShowFrame &&
		a.MD5 == b.MD5
}
