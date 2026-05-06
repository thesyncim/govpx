# libvpx oracle

This directory contains an optional checksum oracle for tests. It builds against
the pinned libvpx v1.16.0 decoder and emits one JSON object per decoded frame.
The Go package does not import cgo or link libvpx.

Build the helper:

```sh
internal/coracle/build_libvpx.sh
```

Build the optional pinned `vpxenc` reference binary for encoder benchmarks:

```sh
internal/coracle/build_vpxenc.sh
```

Run opt-in oracle tests:

```sh
LIBGOPX_WITH_ORACLE=1 LIBGOPX_ORACLE=internal/coracle/build/gopx-vpx-oracle go test ./...
```

Run opt-in extended conformance against external VP8 IVF data:

```sh
LIBGOPX_WITH_ORACLE=1 \
LIBGOPX_ORACLE=internal/coracle/build/gopx-vpx-oracle \
LIBGOPX_TEST_DATA_PATH=/path/to/vp8-ivf-data \
go test .
```

Use `LIBGOPX_TEST_DATA_LIMIT=N` to cap the number of IVF files discovered from
the external data path.

The helper accepts IVF VP8 input:

```sh
internal/coracle/build/gopx-vpx-oracle decode input.ivf
```

Output is newline-delimited JSON:

```json
{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"...","u_md5":"...","v_md5":"...","full_md5":"..."}
```

Run the libgopx encoder benchmark with the optional libvpx comparison:

```sh
LIBGOPX_VPXENC=internal/coracle/build/vpxenc go run ./cmd/gopx-bench
```
