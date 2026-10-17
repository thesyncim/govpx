# Native048 quant checkpoint

Exact head048a5f3e8c8dac4e49acd3fe64bbe2f00e3c6f3e; base4e397136d0370239e4fce6e3ccddc034deaa1a03; oldASM691cd051.
Run https://github.com/thesyncim/govpx/actions/runs/39036118414; native artifact12103105084.
AMD EPYC7763; Go1.27.2; SIMD/AVX2; GOMAXPROCS1; PGOoff; six balanced permutations.

**Do not retain AMD batch unchanged:**8-block +63.866% and public VP8 +2.902% versus base, every paired round slower.

| Batch blocks | ASM ns | Base ns | Current ns | Current/base | Paired current/base range |
| --- | ---: | ---: | ---: | ---: | ---: |
|1|9.9915|15.8350|16.6050|+4.863%|+3.358..+5.000%|
|2|12.8300|25.6000|27.3850|+6.973%|+6.721..+7.263%|
|8|33.0100|89.2500|146.2500|+63.866%|+61.812..+64.144%|
|16|59.4800|174.2000|170.7000|-2.009%|-5.666..-1.838%|
|25|89.1600|277.3500|197.9500|-28.628%|-29.064..-28.292%|

| Workload | ASM ms/input | Base ms/input | Current ms/input | Current/base | Current/ASM |
| --- | ---: | ---: | ---: | ---: | ---: |
|VP9 320x180|7.014312|4.387630|4.384816|-0.064%|-37.488%|
|VP8 720p|23.406658|22.786572|23.447741|+2.902%|+0.176%|
|VP9 720p|60.669778|33.440026|33.378692|-0.183%|-44.983%|

VP9 parent/current paired signs are mixed; no new VP9 gain. VP8 current/ASM +0.176% median is mixed; do not claim assembly is beaten on every pair.
All80 kernels have six zero-allocation samples per variant. All22 non-timing fields and allocations match across18 public workload samples and profile samples. Current-only native-selection/full-signed/alias/short tests pass; current/base SIMD dispatch=true.

Profile is a single120-frame sample,10ms granularity: current QuantizeVP8Batch .15s cumulative of2.75s CPU, base batch .07s of2.68s. Samples under current batch: body .06s, any16 .04s, Int16x8.Less .04s, Max4 .01s. Investigate signed-table validation cost; do not treat sparse samples as exact cycle attribution.

Raw86 files: /private/tmp/govpx-amd64-048a5f3e-artifact. Summary/parity remain sibling JSON files; nothing copied into repository.
