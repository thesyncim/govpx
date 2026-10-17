# Shared-table VP8 batch quantization

Native Apple M4 Max, darwin/arm64, Go 1.27.2, SIMD enabled, PGO off,
GOMAXPROCS=1 and one encoder thread. Public workloads use 720p, 60 input
frames, 2,500 kbps, realtime CPU 8. Six balanced variant permutations run
without competing local Go builds or tests. Parent production codec is
db5b44fe; the assembly baseline is 691cd051. Raw results and drivers are retained.

| VP8 public encode | Median ms/input | Current time difference |
| --- | ---: | ---: |
| Old assembly | 6.840786 | +14.672% |
| Parent SIMD | 7.963865 | -1.499% |
| Shared-table SIMD | 7.844487 | — |

All six current/parent pairs improve (1.19–4.31% less time). All six remain
slower than assembly (8.57–16.00% more time). These are same-run comparisons;
gaps from other runs must not be subtracted to attribute this change's gain.
Every non-timing output field, including quantizer metadata and allocations,
matches. VP8 allocates zero per input. The unchanged VP9 control has mixed
pairs (three faster, three slower), with current/parent medians 12.237217 /
12.108483 ms (+1.063%); no VP9 effect is attributed to this VP8 change.

The kernel keeps shared round, quantization, dequantization and scan vectors
live across blocks. The public dispatcher selects it for batches of at least
eight blocks; one/two-block calls retain their existing path. B.Loop fixtures
validate all outputs against the scalar oracle outside timing, retain an
observable checksum, and report zero bytes and zero allocations.

| Blocks | Assembly ns | Parent ns | Shared-table ns |
| --- | ---: | ---: | ---: |
| 1 | 4.164 | 5.6685 | 5.716 |
| 2 | 5.1405 | 9.180 | 9.118 |
| 8 | 12.500 | 30.590 | 15.710 |
| 16 | 23.970 | 59.160 | 27.950 |
| 25 | 37.180 | 97.875 | 43.480 |

Each cell is a six-sample median. Eight/16/25-block batches improve by
48.6%/52.8%/55.6% versus the parent, and still trail assembly. Small-call
differences are controls, not claimed gains. Earlier safe-index, pointer and
span-guard design measurements are exploratory and do not replace the final
integrated public comparison above.

Bounds and conservative table-alias guards run before any output write.
Negative round/quantization tables fall back to the established scalar path.
The SIMD path supports the complete signed coefficient domain, including
-32768, and preserves sequential block ordering for overlapping input/output.
The independent scalar batch test covers 560 cases across seven batch counts,
signed tables and coefficient extremes. Additional tests cover short buffers,
mutable table aliases, partial overlaps, output sentinels and actual selection.

The ARM kernel uses retained 128-bit vectors and unsigned high multiplication.
Its final unsigned byte-offset hardening generates exactly the same 148 ARM
instructions as the measured binary; the normalized comparison is retained.
The x64 implementation uses 256-bit AVX2 unsigned high multiplication and
retained tables. Required-SIMD Rosetta tests pass, including scalar differential
and selection checks; **native x64 performance is pending CI**. Emulated timings
are not used as performance evidence.

Validation logs retain the full native suite, experiment-off and purego codec
suites, race/checkptr oracles, strict byte parity, 18/18 parity cases, refreshed
PGO and all six build-selection/source gates. The PGO source fingerprint is
`0cd31c57c927dfe7fd2e283f507e985f7c2264c8`. The benchmark binaries use PGO off.
