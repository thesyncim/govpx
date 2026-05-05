# libgopx

`libgopx` is a pure-Go, VP8-only codec library scaffold inspired by libvpx
v1.16.0 and structured for a small Go-style API.

Current status: active VP8 scalar port in progress. Decoder and encoder paths
support a growing subset of VP8, including source-dependent DCPred intra-only
keyframe encoding, but production conformance is not complete yet.

Out of scope:

- VP9
- AV1
- WebM muxing or demuxing in the codec package
- cgo dependency
- full libvpx C API compatibility

Normal tests run without libvpx installed. Optional libvpx smoke tests are
enabled with `LIBGOPX_WITH_ORACLE=1`; set `LIBGOPX_VPXDEC` to a vpxdec binary
from the pinned libvpx version when it is not on `PATH`.
