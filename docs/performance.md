# Performance Notes

This file tracks scalar hot paths and local baselines for the libvpx v1.16.0
VP8 port. Correctness and oracle parity remain the gate before SIMD or unsafe
work.

## Current Baseline

Measured on May 6, 2026 with:

```sh
GOCACHE=/Users/thesyncim/GolandProjects/libgopvx/.gocache \
  go test ./benchmarks -run '^$' -bench 'BenchmarkDecodeLibgopxSmoke$' \
  -benchmem -benchtime=200x

GOCACHE=/Users/thesyncim/GolandProjects/libgopvx/.gocache \
  go test ./benchmarks -run '^$' -bench 'BenchmarkDecodeIntoLibgopxSmoke$' \
  -benchmem -benchtime=200x
```

Environment:

```text
goos: darwin
goarch: arm64
cpu: Apple M4 Max
```

Results on the checked-in libvpx-authored 32x32 two-frame smoke IVF:

| Benchmark | ns/op | frames/s | macroblocks/s | coded MB/s | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| `BenchmarkDecodeLibgopxSmoke-16` | 75489 | 26495 | 105978 | 9.298 | 0 |
| `BenchmarkDecodeIntoLibgopxSmoke-16` | 72936 | 27422 | 109686 | 9.624 | 0 |

## Hot-Path Direction

- Keep scalar decode and encode paths allocation-free before optimizing.
- Profile full-frame decode before changing isolated DSP loops; the frame path
  includes bool decoding, mode/token traversal, reconstruction, loop filtering,
  and border extension.
- Preserve scalar as the reference backend for future SIMD dispatch.
- Add randomized scalar-vs-SIMD equivalence tests before enabling assembly.
- Compare only opt-in live libvpx oracle runs against the pinned v1.16.0 build;
  normal CI must not require libvpx.

## Current Gaps

- Broad external VP8 corpus conformance is still opt-in through
  `LIBGOPX_TEST_DATA_PATH` and is not available in this workspace.
- Full libvpx cyclic/background segmentation and rate-control heuristic parity
  are not complete.
- SIMD/assembly backends have not started; scalar remains the correctness
  backend.
