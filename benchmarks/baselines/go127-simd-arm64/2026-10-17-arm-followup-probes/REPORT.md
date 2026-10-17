# Unselected ARM follow-up probes

Native Apple M4 Max, Go 1.27.2 archsimd, PGO off, GOMAXPROCS=1;
parent a39defa3, merged unchanged as main20bc81cb. These designs remain
research only, with all samples and correctness logs preserved. No >=1.5x
representative component gain was established, so no accepted win is removed.
The separately selected zero-correction filter is reported in its own folder.

* SIMD lane packing instead of scalar shift/OR in incoming vertical-filter
  columns: six paired 250ms B.Loop public Picker comparisons, six cases,
  0.955–0.993x parent; slower despite fewer explicit scalar packing expressions.
* Four parallel exact limited-SAD accumulators sharing row loads: two balanced
  200ms B.Loop rounds, twenty cases, 0.963–1.121x parent. Guard and active-lane
  bookkeeping consume most of the potential source-load saving. Full/early
  existing kernels remain. Exact-row and mutable-alias tests pass.
* Narrow uint16 SAD reductions when h<=16: two three-way 200ms B.Loop rounds,
  sixteen full-scan pointer/x4 cases. 0.955–1.022x parent; full pointer already
  beats assembly, while x4 still trails it. One low parent sample is retained.
* Revisit the previously rejected two-block quantization unroll under the
  component acceptance rule, carrying the current BIC EOB expression: six
  paired 250ms B.Loop public batch comparisons, counts1/2/8/16/25. Speedups
  1.0014/1.0006/0.8972/0.9844/0.9979x. Counts8/16 are slower in all six pairs.
  All outputs match scalar quantization, zero allocations in every sample.
  This check finds no hidden >=1.5x kernel win to rescue from the older
  whole-codec-only rejection.

All four prototypes pass their targeted scalar-oracle tests, with zero
allocations in timed public operations. Small diagnostic samples do not
establish whole-codec effects. Sources, drivers, all raw samples and per-file
SHA256s are retained, including unselected code; no README changes.
