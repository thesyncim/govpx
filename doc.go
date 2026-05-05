// Package libgopx provides a pure-Go VP8 codec API.
//
// The implementation is VP8-only and intentionally excludes VP9, AV1, WebM
// container parsing, cgo, and libvpx C API compatibility. It is structured so
// codec work can be ported incrementally from the frozen libvpx v1.16.0
// baseline while preserving a small Go-style API.
package libgopx
