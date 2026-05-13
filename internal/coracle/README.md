# libvpx oracle

This directory holds optional libvpx-based oracle tools used by tests. The Go
package does not import cgo or link libvpx; the C helpers are built only by the
scripts and are guarded with `//go:build ignore`.

Main tools:

- `vpx_oracle.c`: VP8 decoder checksum oracle.
- `build_vpxenc.sh`: pinned stock `vpxenc` / `vpxdec`.
- `build_vpxdec_vp9.sh`: VP9-enabled `vpxdec` / `vpxenc` for VP9 oracle tests.
- `build_vpxenc_oracle.sh`: patched VP8 encoder trace oracle.
- `oracle_compare.go`: JSON Lines trace comparator.

Run the supported full gate from the repository root:

```sh
make verify-production
```

For decoder-only work:

```sh
make verify-decoder-parity
```

Those targets build the required pinned tools under `internal/coracle/build`,
fetch required VP8/VP9 corpora, set the `GOVPX_*` environment variables, and
run the matching oracle tests.

The VP8 decode helper accepts IVF input:

```sh
internal/coracle/build/govpx-vpx-oracle decode input.ivf
```

Additional VP8 decode modes:

- `decode-postproc`
- `decode-postproc-noise`
- `decode-postproc-all-noise`
- `decode-error-concealment`

Output is newline-delimited JSON:

```json
{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"...","u_md5":"...","v_md5":"...","full_md5":"..."}
```

## Encoder Trace Comparator

The VP8 encoder trace path is compiled only with the `govpx_oracle_trace`
build tag. Normal builds omit it.

Build the patched libvpx trace encoder:

```sh
sh internal/coracle/build_vpxenc_oracle.sh
```

Capture a libvpx trace:

```sh
GOVPX_ORACLE_TRACE_OUT=/tmp/libvpx.jsonl \
  internal/coracle/build/vpxenc-oracle --codec=vp8 input.y4m -o /tmp/out.ivf
```

Compare traces in Go with:

```go
coracle.CompareOracleTraces(govpxR, libvpxR, coracle.CompareOptions{})
```

`make verify-production` builds the patched encoder and runs the supported
trace comparisons.
