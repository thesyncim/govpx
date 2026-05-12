# libvpx oracle

This directory contains optional libvpx-based oracles for tests:

* A decoder MD5 oracle (`vpx_oracle.c`, built by `build_libvpx.sh`).
* A patched stock encoder for parity smoke tests (`build_vpxenc.sh`).
* A patched encoder that emits the same per-frame and per-MB JSON Lines
  oracle trace that govpx's encoder writes when
  `EncoderOptions.OracleTraceWriter` is non-nil
  (`build_vpxenc_oracle.sh`).
* A pure-Go comparator that diffs the two JSON Lines streams field by
  field (`oracle_compare.go` / `oracle_compare_test.go`).

The Go package does not import cgo or link libvpx; the C source files are
guarded with `//go:build ignore` so they are skipped by `go build` and used
only by the shell scripts.

Run the full correctness/parity gate from the repository root:

```sh
make verify-production
```

That target builds `govpx-vpx-oracle`, pinned `vpxenc`, and pinned `vpxdec`
with libvpx optimizations enabled; fetches the libvpx VP8 IVF corpus plus
supported encoder source data under ignored `internal/coracle/build/test-data/`;
and runs all root `TestOracle*` tests with the required/minimum-count corpus
checks enabled. The raw `GOVPX_*` switches remain available inside the
Makefile for targeted debugging, but the supported parity workflow is the
make target.

The decode helper accepts IVF VP8 input:

```sh
internal/coracle/build/govpx-vpx-oracle decode input.ivf
```

Use `decode-postproc` to enable libvpx VP8 deblock/demacroblock/MFQE
postprocessing, `decode-postproc-noise` for ADDNOISE only,
`decode-postproc-all-noise` for deblock/demacroblock/ADDNOISE/MFQE, and
`decode-error-concealment` to initialize libvpx with VP8 error concealment.

Output is newline-delimited JSON:

```json
{"frame":0,"width":16,"height":16,"keyframe":true,"show_frame":true,"y_md5":"...","u_md5":"...","v_md5":"...","full_md5":"..."}
```

Run the govpx encoder benchmark with the optional libvpx comparison
(the bench auto-locates `internal/coracle/build/vpxenc`; pass
`-libvpx-vpxenc=...` to pin a specific binary):

```sh
make oracle-tools
go run ./cmd/govpx-bench
```

## Encoder oracle trace comparator

The govpx encoder emits a per-frame + per-MB JSON Lines oracle trace
through `(*VP8Encoder).SetOracleTraceWriter`. The method is compiled in
only under the `govpx_oracle_trace` build tag; normal builds omit it
entirely. See `../../encoder_oracle_trace.go` for the full schema.

To compare a govpx trace against an equivalent libvpx trace, build the
patched libvpx vpxenc:

```sh
sh internal/coracle/build_vpxenc_oracle.sh
```

The script is idempotent and writes the patched binary to
`internal/coracle/build/vpxenc-oracle`. The patch is inlined in the script
(no separate .patch file in the repo); it adds a single
`vp8/encoder/oracle_trace.c` translation unit plus narrow `extern` hook calls
into `vp8/encoder/encodeframe.c` (per-MB capture inside `encode_mb_row`),
`vp8/encoder/rdopt.c` and `vp8/encoder/pickinter.c` (evaluated inter-candidate
rows), `vp8/encoder/onyx_if.c` (rate/recode and attempt lifecycle), and
`vp8/encoder/bitstream.c` (per-frame flush at the tail of
`vp8_pack_bitstream`). The patch is additive, gates all output on the
`GOVPX_ORACLE_TRACE_OUT` env var, and does not modify any libvpx header.

To capture both traces and diff them:

```sh
# govpx side: the production oracle gate now runs a projected decision trace
# compare via TestOracleEncoderTraceDecisionCompare.
make oracle-test

# libvpx side: run the patched binary with the env var set.
GOVPX_ORACLE_TRACE_OUT=/tmp/libvpx.jsonl \
  internal/coracle/build/vpxenc-oracle --codec=vp8 input.y4m -o /tmp/out.ivf

# Diff: import "github.com/thesyncim/govpx/internal/coracle" and call
# coracle.CompareOracleTraces(govpxR, libvpxR, coracle.CompareOptions{}).
```

`make verify-production` builds both stock `vpxenc` and the patched
`vpxenc-oracle`, exports `GOVPX_VPXENC_ORACLE`, and runs
`TestOracleEncoderTraceDecisionCompare`. The production gate currently compares
a projected frame/rate decision subset (Q, active bounds, zbin-over-quant,
refresh/sign-bias flags, and recode identity), while
`TestOracleEncoderTraceCandidateRowsPresent` separately asserts that govpx and
patched libvpx both emit RD and realtime-fast `inter_candidate` rows.
`TestOracleEncoderTraceInterCandidateCompare` compares a staged VBR panning RD
candidate projection for mode/ref/MV decisions. Broader realtime candidate
comparison, RD/rate scalar tightening, and per-MB residual matching remain
tracked in the VP8 encoder parity plan. The Go-side `CompareOracleTraces`
helper is also covered by `TestCompareOracleTraces*` in
`oracle_compare_test.go` against synthetic JSON Lines inputs, so comparator
regressions still run in the standard `go test ./...` flow without depending on
the patched binary.
