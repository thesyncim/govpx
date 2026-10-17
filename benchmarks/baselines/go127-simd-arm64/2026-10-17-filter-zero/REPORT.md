# ARM zero-correction filter: verified first design

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, GOMAXPROCS=1.
The public VP8 Picker operation filters its macroblock and three internal
edges vertically and horizontally. The exact masked correction f=0 produces
zero in every rounded/saturated correction, so the rest of that edge can be
skipped. Later edges and source advancement remain live; a prior vertical
changed flag is retained. Only ARM generated code changes; AMD is identical.

Fresh six-permutation 250ms B.Loop confirmation, identical eight-case fixture
in current/main/pinned691 assembly, exact full-buffer separate-edge oracle:

| Flat tile pitch | Main/current speedup | ASM/current speedup | Minimum paired main/current |
| --- | ---: | ---: | ---: |
| 32 | 1.829x | 1.655x | 1.738x |
| 1312 | 1.833x | 1.665x | 1.805x |

All twelve flat pairs beat both main and assembly. Every sample is zero B/op
and zero allocs/op. Six smooth/mixed/noop controls range0.997–1.009x main;
active smooth/mixed cases still take about10.6–10.9% more time than assembly.
An initial independent two-way six-round screen also found1.839–1.859x flat
speedups (minimum1.803x); both screens and all unfavorable samples are kept.
Disassembly confirms two UMAXV guards per fused orientation. Instruction
counts are diagnostic only, not performance acceptance evidence.

Whole synthetic VP8 1280x720, realtime CPU8,2500kbps,one thread, balanced
six permutations against the same main and pinned assembly:

* 300frames: -0.705% time versus main (5/6 faster); +7.854% versus assembly (0/6 faster).

* 60frames: +0.108% time versus main (1/6 faster); +11.460% versus assembly (0/6 faster).

All23 non-timing fields match and allocations are zero in every whole sample.
The mixed whole results do not establish ARM gap closure. Retain the >=1.5x
representative flat-tile public component win independently under AGENTS.md.
No gains in unrelated workloads are attributed to the shortcut.

Full native suite, existing scalar filter oracles,384 explicit flat extremes,
cancelling ramps and late-only-active-lane cases, race/checkptr2, generator,
noasm/build-selection/precommit/PGO fingerprint checks pass. Independent
read-only review confirms exact-zero arithmetic and preservation of dependent
vectors/identity-store behavior. Raw logs, all samples, both designs' sources,
identical timed fixtures, disassembly, environment and binary/source hashes
are preserved. The later gradient fixture is NOT substituted for this run's
original eight-case fixture in the archive. No README changes.
