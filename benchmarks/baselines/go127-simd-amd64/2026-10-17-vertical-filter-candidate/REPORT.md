# Native x64 vertical-filter candidate

Parent03afac7c. The previous AMD wrapper declines vertical macro/internal
fusion, so the picker invokes separate macro and internal filter networks.
Generate a vertical combined network from the same canonical arithmetic,
using the existing exact4-byte column packing and carrying modified neighbors.
Capture it before ARM-specific TRN,packed-row and rounded-zero transforms.
ARM generated source and existing AMD horizontal function stay byte-identical.

Initialcols0..7, macrostores1..6, correctedcarry4..7, internaledges8/12/16:
store6..9/10..13/14..17, then load8..11/12..15/16..19 after prior stores.
Maximumaddress19+15*pitch matches the pre-multiplication wrapper proof.
Pitch>=20 prevents row aliasing; masked-off edges keep original carried q.
The existing AVX cleanup and runtime-disable-before-access checks remain.

Expanded pixel oracle covers both orientations (7200 numeric/alignment cases),
independent DSP oracle now covers vertical too, and runtime-disabled guards
check both shapes. Selected AMD correctness passes under Rosetta only; no
emulated speed is used. Independent read-only review finds no correctness
issue. The generated vertical body has1015 instruction lines and calls only
its existing column-load helper plus runtime stack growth; diagnostics do not
establish a speedup. Native x64 CI must measure this candidate before merge.
All old fixtures are retained to expose public/whole regressions. The previous
native EPYC7763 profile samples loopFilterBytes at0.12sflat/0.48scumulative
out of2.36s total; this target selection is qualitative, not causal evidence.
Initial generator concatenation lacked a newline; fixed retry and all logs
are preserved. No README or dependency changes.
