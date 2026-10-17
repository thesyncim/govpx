# Rejected follow-up ARM designs

These candidates are excluded from production. All use the shared-table
quantization codec at 3599f121 as their parent, Apple M4 Max, native ARM,
Go 1.27.2, SIMD enabled, PGO off, GOMAXPROCS=1, one encoder thread and the
same 720p realtime CPU-8 inputs at 2,500 kbps. No local Go builds or tests
compete with timing. Positive deltas mean more encode time.

| Candidate | 60-frame median delta | 300-frame median delta | Decision |
| --- | ---: | ---: | --- |
| Inline initial vertical 16x8 transpose | -0.194% | +0.770% | Short pairs mixed; all six long pairs slower |
| Derive 18/27 filter weights from product 9 | +0.457% | — | Mixed pairs |
| Independent product 27; derive 18 from 9 | -0.149% | — | Mixed pairs |
| Quantization EOB uses BIC instead of MVN/AND | +0.726% | — | Mixed pairs |
| Quantization block pointers share offset arithmetic | +1.838% | — | Mixed pairs |
| Combined BIC and block pointers | +0.449% | — | Mixed pairs |
| Signed clamped neighbor differences below threshold 127 | +1.141% | +1.425% | Mixed pairs; both medians slower |

Transpose, filter-weight and clamped-difference comparisons have six balanced
pairs/permutations. The four-way quantization-loop comparison has eight
balanced permutations. Every non-timing field, including output bytes, frame
counts, quantizer metadata and allocations, matches its same-round parent.
summary.json retains all paired values and medians; drivers and raw reports
are included. These results do not establish gains against old assembly or C.

The transpose variant removes the initial helper call but increases the
vertical body size; the SIMD ABI already returns vectors in registers. Weight
factoring and quantization BIC/address changes reduce some generated work but
do not sustain encoder gains. The clamped variant keeps full-range fallback
functions and specializes only limit/thresh below 127, where signed saturated
differences preserve the decisions; additional conversions/dispatch do not
produce a public benefit. Existing differential tests pass for every design.

Patches reproduce the private candidates against the parent source. Generated
files are modified directly for these experiments; production generators and
dispatch remain unchanged. The separate 600-input CPU profile is diagnostic,
not an acceptance timing: the fused luma API accounts for 22.74% of sampled
CPU, quantization batch for 1.62%. Profile output and the exact public result
are retained. Sampling and instruction counts do not substitute for the
paired public-operation measurements above.
