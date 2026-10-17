# Native x64 gather and vertical filter evidence, PR35

Exact tested34b00fb4, merged asf56c66d7 after all three checks passed. Tested
and merged tree bothb5c3338fbff42d26e526295770a7fb269fba539f. CPU AMD EPYC7763,
Go1.27.2 archsimd,PGOoff,GOMAXPROCS1,six same-host balanced rounds.

| Whole encoder | Time versus main | Time versus old assembly | Faster pairs main/ASM |
|---|---:|---:|---:|
| VP9 320x180 | +1.9072% | -38.2103% | 0/6, 6/6 |
| VP8 720p | -7.6588% | -20.7902% | 6/6, 6/6 |
| VP9 720p | +0.6882% | -45.3178% | 2/6, 6/6 |

VP8 is the actual gather/filter candidate; VP9 is a retained control with
measured regressions versus main. No VP9 improvement or causal attribution is
claimed. Both VP9 workloads still beat old assembly in every paired sample.

Component evidence is independent of whole timing: all18 new/original public
gather fixtures beat main by at least1.993x in every paired sample, zero B/op
and allocations. New8x8 medians2.075–2.087xmain, near assembly parity; new16x16
2.008–2.016xmain, about5%more time than assembly. Original8x8 beats assembly
1.060x; avoid a blanket claim that every x64 gather beats assembly. All10
Picker fusion fixtures and all60 pairs improve versus main,1.082–1.355x
medians; active cases still0.7–4.9%more time than assembly. Noop controls beat
assembly1.608–1.698x. Keep qualifying kernel wins even if whole timing is flat.

All206 kernels have six samples per variant and zero B/op/allocs. All22
non-allocation semantic fields match across all public rounds and profiles.
Current/main allocation samples match throughout. Preserve the old-assembly
VP9720p round6 allocation outlier1.266667/input versus1.25 elsewhere; it is not
a candidate allocation regression. Keep all77 median slower kernel controls,
nine all-six-slower results, original timing outliers and raw profiles.

Native validation:84 top-level tests plus6 panic probes,118 total PASS lines,
no skips/failures; required3 selected-helper and3 public gather tests execute.
All five recorded source hashes match exact tested HEAD. ARM results belong to
a separate M4Max report; do not infer ARM speed or compare absolute cross-CPU
latencies from this native x64 job.

Separate optimized-C comparison on AMD EPYC9V45: fixed-quality VP8 encode time
2.415902x libvpxv1.16.0 (+141.5902% time; libvpx is faster), with matched measuredPSNR/SSIM/payload/frame/drop
counts. This does not prove bitstream identity. Adaptive VP8 and VP9 do different
quality/output work and retain their full controls/quality evidence; no equivalent
speed claim. Never add this ratio to assembly or main-relative gains.

Complete86 native and74 C raw files, all samples/allocations/semantic fields,
profiles, source/dispatch provenance and SHA256 manifests are included. Existing
outer manifests cover the original archived evidence; this report summarizes it.
README files remain unchanged.
