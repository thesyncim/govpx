# Exact-row SIMD squared error: native results

Tested `6a7b1444765e30fcbcc4a5aca8abd1af73dd9013` against `f3a02aee81979d0c822f5814343f2287d9d86194` (tree-equivalent to merged main `faee3c2f`). Native CPU: **EPYC 9V74**, Go 1.27.2, PGO off. All three checks pass and all eleven source hashes match.

All 282 kernels have six samples per variant and zero bytes/allocations. There are 117 targeted checks / 262 PASS lines, no failures/skips. All 22 semantic fields match. The baseline VP9 720 round3 allocation outlier 1.266667 instead of modal1.25 is retained; current/profile samples match their controls.

**Whole VP8 is +0.881638% slower than baseline in all six pairs**, and +2.274433% versus ASM. Keep the verified component gains under the user’s policy; no whole-codec gain is claimed. VP9 timings are unchanged-source controls: 320 +.296920% baseline (4/6 faster), 720 -.392144% (3/6).

All 36 SSE cases improve in every pair. Twenty-seven exceed 1.5x baseline in every pair, minimum1.691x. Slice APIs beat ASM; pointer8x8 remains 27.47–27.64% slower than ASM. The original SSE8x8 fixture improves 1.884x baseline / 1.201x ASM in all pairs. All raw pairs remain in the audit.

| Fixture | Base ns | Current ns | Base/current | Minimum pair | ASM/current |
|---|---:|---:|---:|---:|---:|
| pointer8x8/pitch1312-1320/pattern0 | 17.2150 | 10.1050 | 1.7036x | 1.6996x | 0.7844x |
| pointer8x8/pitch1312-1320/pattern1 | 17.2200 | 10.1050 | 1.7041x | 1.6916x | 0.7845x |
| pointer8x8/pitch1312-1320/pattern2 | 17.2150 | 10.1050 | 1.7036x | 1.6976x | 0.7835x |
| pointer8x8/pitch32-37/pattern0 | 17.2150 | 10.1050 | 1.7036x | 1.7003x | 0.7835x |
| pointer8x8/pitch32-37/pattern1 | 17.2150 | 10.1000 | 1.7045x | 1.7003x | 0.7839x |
| pointer8x8/pitch32-37/pattern2 | 17.2200 | 10.1000 | 1.7050x | 1.7020x | 0.7838x |
| pointer8x8/pitch8-8/pattern0 | 17.2200 | 10.1050 | 1.7041x | 1.6932x | 0.7844x |
| pointer8x8/pitch8-8/pattern1 | 17.2200 | 10.1100 | 1.7033x | 1.6999x | 0.7843x |
| pointer8x8/pitch8-8/pattern2 | 17.2150 | 10.1100 | 1.7028x | 1.6996x | 0.7839x |
| slice8x16/pitch1312-1320/pattern0 | 29.7800 | 15.0250 | 1.9820x | 1.9754x | 1.4276x |
| slice8x16/pitch1312-1320/pattern1 | 29.8050 | 15.0200 | 1.9844x | 1.9800x | 1.4281x |
| slice8x16/pitch1312-1320/pattern2 | 29.7950 | 15.0200 | 1.9837x | 1.9787x | 1.4281x |
| slice8x16/pitch32-37/pattern0 | 29.7700 | 15.0150 | 1.9827x | 1.9800x | 1.4269x |
| slice8x16/pitch32-37/pattern1 | 29.7900 | 15.0200 | 1.9834x | 1.9813x | 1.4274x |
| slice8x16/pitch32-37/pattern2 | 29.7900 | 15.0250 | 1.9827x | 1.9781x | 1.4270x |
| slice8x16/pitch8-8/pattern0 | 29.7650 | 15.0250 | 1.9810x | 1.9774x | 1.4260x |
| slice8x16/pitch8-8/pattern1 | 29.7800 | 15.0150 | 1.9833x | 1.9800x | 1.4272x |
| slice8x16/pitch8-8/pattern2 | 29.7500 | 15.0100 | 1.9820x | 1.9794x | 1.4270x |
| slice8x4/pitch1312-1320/pattern0 | 10.1100 | 7.2475 | 1.3950x | 1.3721x | 1.2820x |
| slice8x4/pitch1312-1320/pattern1 | 10.1100 | 7.2540 | 1.3937x | 1.3846x | 1.2814x |
| slice8x4/pitch1312-1320/pattern2 | 10.1050 | 7.2355 | 1.3966x | 1.3622x | 1.2841x |
| slice8x4/pitch32-37/pattern0 | 10.1050 | 7.3385 | 1.3770x | 1.3468x | 1.2659x |
| slice8x4/pitch32-37/pattern1 | 10.1000 | 7.2375 | 1.3955x | 1.3605x | 1.2835x |
| slice8x4/pitch32-37/pattern2 | 10.1150 | 7.2795 | 1.3895x | 1.3777x | 1.2776x |
| slice8x4/pitch8-8/pattern0 | 10.1000 | 7.2630 | 1.3906x | 1.3617x | 1.2787x |
| slice8x4/pitch8-8/pattern1 | 10.1050 | 7.2865 | 1.3868x | 1.3689x | 1.2756x |
| slice8x4/pitch8-8/pattern2 | 10.1100 | 7.2025 | 1.4037x | 1.3976x | 1.2894x |
| slice8x8/pitch1312-1320/pattern0 | 16.6550 | 9.5555 | 1.7430x | 1.7391x | 1.1726x |
| slice8x8/pitch1312-1320/pattern1 | 16.6700 | 9.5610 | 1.7435x | 1.7408x | 1.1730x |
| slice8x8/pitch1312-1320/pattern2 | 16.6700 | 9.5580 | 1.7441x | 1.7406x | 1.1718x |
| slice8x8/pitch32-37/pattern0 | 16.6500 | 9.5595 | 1.7417x | 1.7385x | 1.1716x |
| slice8x8/pitch32-37/pattern1 | 16.6500 | 9.5610 | 1.7414x | 1.7394x | 1.1709x |
| slice8x8/pitch32-37/pattern2 | 16.6600 | 9.5525 | 1.7440x | 1.7396x | 1.1730x |
| slice8x8/pitch8-8/pattern0 | 16.6500 | 9.5540 | 1.7427x | 1.7308x | 1.1733x |
| slice8x8/pitch8-8/pattern1 | 16.6550 | 9.5575 | 1.7426x | 1.7313x | 1.1724x |
| slice8x8/pitch8-8/pattern2 | 16.6600 | 9.5555 | 1.7435x | 1.7417x | 1.1726x |

All 91 median regressions and five all-six-slower controls are retained:

- `vp8/dsp/BenchmarkSADLimitSliceNative/pitch32/extremefalse/near`: +0.593824% baseline.
- `vp8/dsp/BenchmarkSADLimitSliceNative/pitch32/extremetrue/bound`: +0.183234% baseline.
- `vp8/dsp/BenchmarkSADLimitSliceNative/pitch32/extremetrue/near`: +0.288323% baseline.
- `vp8/dsp/BenchmarkUVRoundedZeroNative/pitch672/verticalfalse/kind4/smooth`: +0.317228% baseline.
- `vp9/dsp/BenchmarkVP9SubpelPointerNative/16x8/phase3-5/paddedfalse`: +0.134048% baseline.

M4 Max evidence is separate: nine8x16 cases gain 1.6253–1.6423x prototype parent `1e8809b6`, all54 pairs >=1.5802x. Both the initial60-frame +1.4459% regression and fresh60-frame -.6109% gain remain; whole effect is uncertain and ARM ASM deficits remain. After rebase onto repaired f3, all228 ARM instruction words match the timed prototype.

Optimized C is a separate EPYC 7763 job. Fixed VP8 Go takes **2.232940x libvpx time** (+123.2940%), matching measured PSNR/SSIM/payload/counts, without proving bitstream identity. Adaptive +70.1504%, VP9 +197.2228/+209.8442% compare unequal quality/output work. Every quality-pass output statistic matches its timed output. All reference timers are `vpxenc-stats`; pinned ASM/SSE2/AVX2/runtime dispatch are enabled. No cross-host attribution.

Merged as ordinary two-parent commit `8ee816b14603fd34dccbcb629f021cf5ee57e396`, parents mainfaee and tested6a, preserving tested tree `de3f9125e8ebc8970ef634a2d08e47fd577dd690`. A normal fast-forward push resolved squash-ancestry binary-PGO conflict; no force, protection override or PR-head rewrite. GitHub38 became MERGED with head6a unchanged; main/tree/parents verified before only its exact remote source branch was deleted. Root checkout/index/local refs remain untouched.

Audit, scope and archives retain every output, allocation exception, source hash and merge proof. Member and outer SHA256 manifests verify all160 raw files, including original whitespace. No local Go, source or README changes during this audit.
