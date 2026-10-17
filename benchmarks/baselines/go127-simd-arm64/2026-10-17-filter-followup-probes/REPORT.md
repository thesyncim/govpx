# Follow-up filter probes, not selected

Native Apple M4 Max, Go1.27.2 archsimd, PGO off, GOMAXPROCS1. Parent is
PR33 exact42bac3ce. These diagnostic probes produced no new1.5x representative
component gain and are excluded from production. The accepted chroma win is
preserved separately in2026-10-17-uv-rounded-zero regardless of whole timing.

Wrapping f+bound with unsigned comparison instead of ABS/UMAXV: two balanced
200ms rounds mostly0.947–1.018x parent. Removing the earlier need-mask branch
so only the final correction test remains:0.912–1.030x; noop loses6–9%.
IMPORTANT: raw shared driver labels are historical: `assembly` is the BITS
experiment and `candidate` is FOLD. There is NO assembly control in that test.
The corrected summary maps the labels explicitly; no ASM inference is made.

Selective luma shortcut only when all16 mask lanes are eligible: two250ms
component pairs are mixed, with flat0.9555/0.9911x parent and ramp0.9886/0.9786x.
The two-round four-way300-frame encoder diagnostic has identical23 non-timing
fields and zero allocations; UV+0.283%, selective luma+0.121% encode time
versus parent. It is diagnostic, not fresh acceptance evidence. Keep all raw
samples and the rejected source variants; do not use these noisy results to
discard already verified component gains.

The1800-frame parent CPU profile is qualitative, collected outside every timing
window:13.03s samples/13.61s duration. FusedY vertical11.67% and horizontal6.75%
cumulative, UV3.45%, DCT batch4.60%, full SAD quad0.92%, entropy packing5.91%.
These are samples, not isolated causal measurements or predicted speedups.
The x64 target list is a read-only reanalysis of PR32's EPYC9V74 native raw
component samples, with all154 median current/ASM time ratios. Those samples
are fully archived under the preceding native audit; no new x64 speed claim.
