# libgopx

`libgopx` is a pure-Go, VP8-only codec library scaffold inspired by libvpx
v1.16.0 and structured for a small Go-style API.

Current status: repository/API foundation only. Codec algorithms are not yet
ported, so encoder and decoder frame processing methods return
`ErrUnsupportedFeature` after validating their inputs.

Out of scope:

- VP9
- AV1
- WebM muxing or demuxing in the codec package
- cgo dependency
- full libvpx C API compatibility

Normal tests must run without libvpx installed. Future libvpx oracle tests will
be opt-in through `LIBGOPX_WITH_ORACLE=1`.
