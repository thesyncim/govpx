# libvpx oracle

This directory holds optional libvpx-based oracle tools used by tests. The Go
package does not import cgo or link libvpx; the C helpers are built only by the
scripts and are guarded with `//go:build ignore`.

Main tools:

- `vpx_oracle.c`: VP8 decoder checksum oracle.
- `build_vpxenc.sh`: pinned stock `vpxenc` / `vpxdec` (VP8 path).
- `build_vpxdec_vp9.sh`: VP9-enabled `vpxdec` / `vpxenc` for Profile 0
  oracle tests. The `vpxdec` build is lightly patched to expose VP9 decoder
  controls that stock `vpxdec` lacks CLI flags for, including
  `VP9_SET_SKIP_LOOP_FILTER`, `VP9_INVERT_TILE_DECODE_ORDER`, and explicit
  VP9 postprocess flag selection.
- `build_vpxenc_vp9_frameflags.sh`: VP9 per-frame flag encoder driver used
  by byte-parity / scoreboard / BD-rate tests that need to push
  `VPX_EFLAG_FORCE_KF` / `VP8_EFLAG_NO_REF_*` / `VP8_EFLAG_FORCE_*` and
  runtime control transitions per frame.
- `build_vpxenc_oracle.sh`: patched VP8 encoder trace oracle.
- `build_vpxenc_frameflags.sh`: VP8 per-frame flag encoder driver (links
  against the patched `build_vpxenc_oracle.sh` libvpx.a).
- `build_libvpx_vp9.sh`: VP9-enabled libvpx + VP9 DSP oracle binary used
  to regenerate the committed DSP-corpus testdata.
- `oracle_compare.go`: JSON Lines trace comparator.

## Building the oracle binaries

Run any single script directly:

```sh
sh internal/coracle/build_vpxenc.sh
sh internal/coracle/build_vpxdec_vp9.sh
sh internal/coracle/build_vpxenc_vp9_frameflags.sh
sh internal/coracle/build_vpxenc_oracle.sh
sh internal/coracle/build_vpxenc_frameflags.sh
sh internal/coracle/build_libvpx.sh
sh internal/coracle/build_libvpx_vp9.sh
```

Each script is idempotent: it stamps the configure flags into the source
tree under `internal/coracle/build/` and skips the rebuild when the
stamp matches.

Convenience targets (from the repository root):

```sh
make oracle-bins        # builds all oracle binaries in one shot
make oracle-tools       # VP8 oracle + patched trace encoder + frameflags
make vp9-vpxdec-tools   # VP9 vpxdec + vpxenc + vpxenc-vp9-frameflags
```

The supported full gate from the repository root:

```sh
make verify-production
```

For decoder-only work:

```sh
make verify-decoder-parity
```

Those targets build the required pinned tools under `internal/coracle/build`,
fetch required VP8 corpora and VP9 Profile 0 fixtures, set the `GOVPX_*`
environment variables, and run the matching oracle tests.

## Environment variables

The Go test harness gates every oracle/byte-parity test on the
`GOVPX_WITH_ORACLE=1` umbrella flag and resolves each binary through a
matching `GOVPX_*_BIN` / `GOVPX_*` override. When a `GOVPX_*_BIN` env var
is unset the helper falls back to `internal/coracle/build/<binary-name>`
relative to the package source, so callers that run the build scripts
without overrides get the default layout for free.

Umbrella gate:

| Variable             | Purpose                                                                                       |
| -------------------- | --------------------------------------------------------------------------------------------- |
| `GOVPX_WITH_ORACLE`  | Must be `1` to enable oracle / byte-parity / scoreboard tests. Otherwise those tests t.Skip.  |

Binary paths consumed by tests / helper packages:

| Variable                                | Default binary                                  | Built by                                   | Used by                                                                                |
| --------------------------------------- | ----------------------------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------- |
| `GOVPX_ORACLE`                          | `build/govpx-vpx-oracle`                        | `build_libvpx.sh`                          | VP8 decoder checksum oracle (`vp8_decoder_checksum_helpers_test.go`, `benchmarks/decode_test.go`). |
| `GOVPX_ORACLE_BIN`                      | `build/govpx-vpx-oracle`                        | `build_libvpx.sh`                          | Override consumed by `build_libvpx.sh` when relocating the oracle binary.              |
| `GOVPX_VPXENC`                          | `build/vpxenc`                                  | `build_vpxenc.sh`                          | VP8 vpxenc reference for byte-parity tests.                                            |
| `GOVPX_VPXENC_BIN`                      | `build/vpxenc`                                  | `build_vpxenc.sh`                          | Override consumed by `build_vpxenc.sh`.                                                |
| `GOVPX_VPXDEC`                          | `build/vpxdec`                                  | `build_vpxenc.sh`                          | VP8 vpxdec reference for output-parity tests.                                          |
| `GOVPX_VPXDEC_BIN`                      | `build/vpxdec`                                  | `build_vpxenc.sh`                          | Override consumed by `build_vpxenc.sh`.                                                |
| `GOVPX_VPXENC_ORACLE`                   | `build/vpxenc-oracle`                           | `build_vpxenc_oracle.sh`                   | Patched VP8 encoder trace oracle (JSONL).                                              |
| `GOVPX_VPXENC_ORACLE_BIN`               | `build/vpxenc-oracle`                           | `build_vpxenc_oracle.sh`                   | Override consumed by `build_vpxenc_oracle.sh`.                                         |
| `GOVPX_VPXENC_FRAMEFLAGS`               | `build/vpxenc-frameflags`                       | `build_vpxenc_frameflags.sh`               | VP8 per-frame flag driver used by byte-parity tests.                                   |
| `GOVPX_VPXENC_FRAMEFLAGS_BIN`           | `build/vpxenc-frameflags`                       | `build_vpxenc_frameflags.sh`               | Override consumed by `build_vpxenc_frameflags.sh`.                                     |
| `GOVPX_VPXDEC_VP9_BIN`                  | `build/vpxdec-vp9`                              | `build_vpxdec_vp9.sh`                      | VP9 vpxdec reference (`internal/coracle.VpxdecVP9Path`).                                |
| `GOVPX_VPXENC_VP9_BIN`                  | `build/vpxenc-vp9`                              | `build_vpxdec_vp9.sh`                      | VP9 vpxenc reference (`internal/coracle.VpxencVP9Path`).                                |
| `GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN`       | `build/vpxenc-vp9-frameflags`                   | `build_vpxenc_vp9_frameflags.sh`           | VP9 per-frame flag driver (`internal/coracle.VpxencVP9FrameFlagsPath`).                |
| `GOVPX_VPX_TEMPORAL_SVC_ENCODER`        | `build/vpx_temporal_svc_encoder`                | `build_vpxenc.sh`                          | VP8 temporal SVC oracle.                                                               |
| `GOVPX_VPX_TEMPORAL_SVC_ENCODER_BIN`    | `build/vpx_temporal_svc_encoder`                | `build_vpxenc.sh`                          | Override consumed by `build_vpxenc.sh`.                                                |
| `GOVPX_VP9_DSP_ORACLE_BIN`              | `build/govpx-vp9-dsp-oracle`                    | `build_libvpx_vp9.sh`                      | VP9 DSP oracle (regenerates committed `internal/vp9/dsp/testdata`).                    |
| `GOVPX_VP9_SPATIAL_SVC_ENCODER`         | `build/vp9_spatial_svc_encoder`, PATH, or libvpx examples tree | (manual build, libvpx examples tree) | VP9 spatial SVC oracle (`internal/coracle.VP9SpatialSVCEncoderPath`).                 |

Build-tree layout:

| Variable                       | Purpose                                                                                       |
| ------------------------------ | --------------------------------------------------------------------------------------------- |
| `GOVPX_CORACLE_BUILD_DIR`      | Override the `internal/coracle/build/` root used by every build script.                       |
| `GOVPX_LIBVPX_PREFIX`          | Install prefix for `build_libvpx.sh`.                                                         |
| `GOVPX_LIBVPX_VP9_PREFIX`      | Install prefix for `build_libvpx_vp9.sh`.                                                     |
| `GOVPX_LIBVPX_LIBS`            | Link flags for `build_libvpx.sh` (`-lvpx -lm -pthread` by default).                            |
| `GOVPX_LIBVPX_VP9_LIBS`        | Link flags for `build_libvpx_vp9.sh` (`-lvpx -lm -pthread` by default).                        |

BD-rate gate (`make verify-bd-rate`) reads additional env vars:

| Variable                             | Purpose                                                                                          |
| ------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `GOVPX_BD_RATE_GATES`                | Must be `1` for the slow VP8/VP9 BD-rate quality gates to run.                                   |
| `GOVPX_BD_RATE_BUILD_LIBVPX`         | When `1`, missing `vpxenc-vp9-frameflags` triggers a one-shot libvpx build.                       |
| `GOVPX_BD_RATE_LIBVPX_REQUIRED`      | When `1`, missing libvpx oracle is `t.Fatal` instead of `t.Skip` (CI guard).                     |
| `GOVPX_VPXENC_VP8_BIN`               | Optional VP8 `vpxenc` path for the VP8 BD-rate gates.                                            |
| `GOVPX_BD_RATE_LIBVPX_VP8_REQUIRED`  | When `1`, missing VP8 `vpxenc` is `t.Fatal` instead of `t.Skip` for VP8 BD-rate gates.            |

## VP8 decode helper

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

For large recode-loop forensics, scope per-iteration inter-candidate rows with
any combination of:

```sh
GOVPX_ORACLE_INTER_CANDIDATE_FRAME=2
GOVPX_ORACLE_INTER_CANDIDATE_ITER=23
GOVPX_ORACLE_INTER_CANDIDATE_MB_ROW=5
GOVPX_ORACLE_INTER_CANDIDATE_MB_COL=2
```

Set `GOVPX_ORACLE_NEWMV_PICKER=1` to include picker-side quantize rows for
NEWMV luma (`newmv_picker_quantize`) and inter-candidate UV
(`picker_uv_quantize`). The frame/iter/MB filters above apply to these rows as
well, which keeps recode-loop traces small enough for single-MB bisection.

Compare traces in Go with:

```go
coracle.CompareOracleTraces(govpxR, libvpxR, coracle.CompareOptions{})
```

`make verify-production` builds the patched encoder and runs the supported
trace comparisons.
