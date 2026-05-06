# libvpx oracle

This directory contains an optional checksum oracle for tests. It builds against
the pinned libvpx v1.16.0 decoder with VP8 postprocess and error-concealment
support enabled, then emits one JSON object per decoded frame. The Go package
does not import cgo or link libvpx.

Run the full correctness/parity gate from the repository root:

```sh
make verify-production
```

That target builds `govpx-vpx-oracle`, pinned `vpxenc`, and pinned `vpxdec` with
libvpx optimizations enabled; fetches the libvpx VP8 IVF corpus plus supported
encoder source data under ignored `internal/coracle/build/test-data/`; and runs
all root `TestOracle*` tests with the required/minimum-count corpus checks
enabled. The raw
`GOVPX_*` switches remain available inside the Makefile for targeted
debugging, but the supported parity workflow is the make target.

The helper accepts IVF VP8 input:

```sh
internal/coracle/build/govpx-vpx-oracle decode input.ivf
```

Use `decode-postproc` to enable libvpx VP8 deblock/demacroblock/MFQE
postprocessing, and `decode-error-concealment` to initialize libvpx with VP8
error concealment.

Output is newline-delimited JSON:

```json
{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"...","u_md5":"...","v_md5":"...","full_md5":"..."}
```

Run the govpx encoder benchmark with the optional libvpx comparison:

```sh
make oracle-tools
GOVPX_VPXENC=internal/coracle/build/vpxenc go run ./cmd/govpx-bench
```
