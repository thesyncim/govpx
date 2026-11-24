package common

import "errors"

// Stable Go errors for VP8 frame validation modeled on libvpx v1.16.0
// vp8/common/alloccommon.c frame-size checks.

var ErrInvalidFrameSize = errors.New("govpx: invalid VP8 frame size")
