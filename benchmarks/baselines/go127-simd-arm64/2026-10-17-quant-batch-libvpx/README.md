# Shared-table SIMD versus optimized libvpx on ARM

Apple M4 Max, native darwin/arm64, Go 1.27.2, SIMD enabled, PGO off,
GOMAXPROCS=1 and one encoder thread. The Go production codec is 3599f121.
The pristine optimized libvpx v1.16.0 binary is unchanged from the earlier
ARM reference build: NEON, dot product, I8MM and runtime CPU dispatch enabled.
Source validation, compiler, configuration, object list and binary hashes are
retained. All timing and quality JSON here is newly measured for this Go codec.

| Workload | Go ms/input | libvpx ms/input | Go time difference | Encoded/dropped, both |
| --- | ---: | ---: | ---: | --- |
| VP9 320x180 | 1.567647 | 0.617100 | +154.03% | 117/3 |
| VP9 1280x720 | 12.061530 | 4.599933 | +162.21% | 48/12 |
| VP8 1280x720 adaptive | 7.699581 | 4.730708 | +62.76% | 58/2 |
| VP8 1280x720 fixed | 6.509613 | 3.011758 | +116.14% | 58/2 |

Six rounds per workload alternate Go-first and reference-first execution after
discarded warmups. Inputs and corresponding realtime CBR, quantizer, GOP,
buffering, drop, denoising and speed controls match. Go timing sums EncodeInto
calls; C timing uses stock vpxenc's internal vpx_codec_encode timer. Input
frames, including dropped inputs, are the denominator. File/subprocess wall
times are recorded separately; legacy C latency quantiles repeat the mean and
are not used. No local Go build/test competed with this measurement.

A separate untimed decoded-quality pass matches every timing run's output
statistics. VP8 adaptive CPU 8 and fixed CPU -8 are distinct workloads. Within
each, measured PSNR, SSIM, payload size and encoded/drop counts agree between
Go and C. This does not by itself prove bitstream identity. Adaptive VP8 PSNR
is 27.983951, SSIM 0.908503948 and payload 685,957 bytes; fixed VP8 PSNR is
27.157611, SSIM 0.925927329 and payload 723,593 bytes.

VP9 has small output/quality differences and is a matched-input/control
comparison, not a claim of identical coding work:

| Workload | PSNR Go/C | SSIM Go/C | Payload Go/C |
| --- | --- | --- | --- |
| VP9 320x180 | 28.910974/28.969583 | 0.966373405/0.967011486 | 246893/246912 |
| VP9 1280x720 | 27.516434/27.546117 | 0.949347795/0.949095623 | 642654/643059 |

All six Go/C pairs are slower in every workload. Absolute times from older
runs or other CPUs are not used to attribute this change's gain. Reproduce
with scripts/benchmark-libvpx.py and the optimized dual-codec vpxenc recorded
by build-arm.sh; environment.json identifies the new Go binary and unchanged
C build. The remaining optimized-C gap is separate from the old-Go-assembly
comparison in the adjacent quant-batch evidence directory.
