# libvpx oracle

This directory contains an optional checksum oracle for tests. It builds against
the pinned libvpx v1.16.0 decoder and emits one JSON object per decoded frame.
The Go package does not import cgo or link libvpx.

Build the helper:

```sh
internal/coracle/build_libvpx.sh
```

Run opt-in oracle tests:

```sh
LIBGOPX_WITH_ORACLE=1 LIBGOPX_ORACLE=internal/coracle/build/gopx-vpx-oracle go test ./...
```

The helper accepts IVF VP8 input:

```sh
internal/coracle/build/gopx-vpx-oracle decode input.ivf
```

Output is newline-delimited JSON:

```json
{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"...","u_md5":"...","v_md5":"...","full_md5":"..."}
```
