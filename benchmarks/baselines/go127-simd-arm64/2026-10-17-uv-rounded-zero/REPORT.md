# ARM chroma rounded-zero correction shortcut

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, GOMAXPROCS=1.
Parent is PR33 exact42bac3ce; pinned assembly691cd051. After masking the
canonical signed correction, return for kind6 |f|<=2 or kind4 |f|<=3.
Every primary/secondary and macro weighted correction then rounds to zero.
Unsigned ABS retains -128 as128, so it stays active. Existing wrapper requires
disjoint U/V allocations and logical rows. No carried edge state is omitted.
Larger corrections retain original arithmetic and store order.

Six fresh permutations of assembly/parent/current,150ms B.Loop. The identical
32-case fixture is copied to every snapshot: pitches32/672, both orientations,
internal/macro edges, flat/gradient/smooth/noop tiles, unaligned U3/V5 and minimal
readable8x8 windows with guards. Public DSP calls stay live through function
variables selected before B.Loop. Independent per-plane scalar oracles compare
the complete backing buffers. Only cases that change pixels restore both tiles
per iteration; identity cases repeat the same unchanged inputs. All samples
have zero bytes/op and zero allocations/op.

The16 flat/gradient cases improve 1.586–2.288x parent. All96 paired speedups are at least 1.529x. The same cases improve 1.377–1.512x ASM; all96 pairs beat ASM. These are fixture-scoped component wins, retained under AGENTS.md independently of whole-codec timing.

The16 smooth/noop controls range 0.9727–1.0054x parent; preserve every slower sample and regression. No general textured-filter speedup is claimed.

Whole VP8 synthetic1280x720,realtime CPU8,2500kbps,one thread, six balanced three-way permutations:

* 300frames: +0.013% encode time versus parent (3/6 faster), +11.746% versus ASM (0/6 faster).

* 60frames: +0.306% encode time versus parent (3/6 faster), +12.184% versus ASM (0/6 faster).

All23 non-timing result fields match and whole-codec allocations remain zero.
The ARM assembly gap remains open. No component gain is used as a whole-codec
claim. The two-round four-way luma/chroma diagnostic is archived separately;
it is not the acceptance run. Selective luma mask gating is not selected.

Correctness: full native ./..., original scalar filter tests,1280 public
boundary/alignment/plane-tail cases, exhaustive256 signed corrections in all16
lanes for both kinds, race/checkptr2, and six fixed-speed packet/drop digests
matching parent. Independent review confirms rounding, aliases and generator.
Initial snapshot full-suite failure was missing ignored upstream oracle source;
after linking the existing pinned source both tests and full suite passed.
Initial AMD build failure exposed an ARM-only ReduceMax method in a runtime
GOARCH branch; final architecture-specific helpers fix that compile issue.
Both initial failures and final passing logs remain archived.

Codegen: ARM final helper fully inlines and exactly matches the timed prototype's
410 instruction lines. AMD's constant-false helper removes the shortcut and its
464 instruction lines exactly match parent. Address prefixes alone are removed;
instruction counts are asserted nonzero. Rosetta checks correctness only.
Generator regeneration and final root PGO/fingerprint/build-selection checks
are recorded alongside their logs. No README changes or dependencies added.
