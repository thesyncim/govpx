package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// FirstByteDiff returns the byte offset of the first divergence between a and
// b. It returns -1 when the slices match exactly.
func FirstByteDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

// MatchedFramePrefixLength returns the largest N such that got[:N] and
// want[:N] have matching SHA-256 sums frame-by-frame.
func MatchedFramePrefixLength(got, want [][]byte) int {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if sha256.Sum256(got[i]) != sha256.Sum256(want[i]) {
			return i
		}
	}
	return n
}

// FramePayloadSHA8s returns per-frame "<sha8>:<len>" summaries.
func FramePayloadSHA8s(frames [][]byte) []string {
	out := make([]string, len(frames))
	for i, frame := range frames {
		sum := sha256.Sum256(frame)
		out[i] = hex.EncodeToString(sum[:8]) + ":" + strconv.Itoa(len(frame))
	}
	return out
}
