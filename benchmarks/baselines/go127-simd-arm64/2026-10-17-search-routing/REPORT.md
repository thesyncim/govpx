# Search-routing experiments, retained as research only

Parent a39defa388e7b93a84f42ab00887c0febc84bb8b; native Apple M4 Max,
Go 1.27.2 archsimd, PGO off, one encoder thread, GOMAXPROCS=1.
All ten quad-search callers precheck each SAD strictly against a best cost
that can only decrease during the quad. Limited row-prefix witnesses above
that cutoff therefore preserve candidate selection. Clamping happens before
int32 conversion; nonpositive cutoffs skip SAD only after reference validation
and source-clamp setup, preserving scratch and context side effects.

The two-round 720p/300-frame diagnostic rejects unconditional limited SAD
(1.69% more time) and the <=4096 threshold design (0.78% more time). They
remain private experiments. The narrower zero-bound shortcut was retested in
six balanced permutations against the same parent and old assembly:
300 frames took 2.206% more time than parent (3/6 faster), 14.310% more than
assembly; 60 frames took 1.784% less than parent (5/6 faster), 10.338% more
than assembly. All samples, including the large parent outlier, are retained.
These mixed desktop measurements do not establish a sustained encoder win.
No representative >=1.5x component win was established for this routing;
the separately accepted limited-SAD kernels remain in production.

The 23 non-timing result fields match in every whole-codec sample and
allocations stay zero. Independent scalar threshold tests cover exact-prefix
boundaries, integer extremes, clamped source blocks, invalid references and
decline side effects. The zero-bound and <=4096 variants match parent packet
bytes AND drop positions across six streams: partial 17x16, flat/patterned
64x64, 320x180, and 720p/60 and /300. Packet digests include input index,
payload length, dropped flag and payload bytes. They are correctness tests,
not performance timings. The unconditional variant DOES NOT match the parent
digest for 320x180/120 frames, despite matching the five other streams and the
720p timing statistics. An identical-source retry matches the parent digest.
Positive CpuUsed=8 permits wall-clock adaptive speed selection at serial
320x180: vp8_encoder_config.go:772 disables this with CpuUsed<0, while
interFrameAutoSpeedTimingCompensation leaves 240-MB single-thread work unpinned.
All four variants therefore also run the same six streams with fixed
CpuUsed=-8; every packet/drop digest matches. This supports search equivalence
under fixed decisions and identifies adaptive timing as a plausible cause of
the initial divergence, without proving the exact dynamic speed history.
Both original and retry hashes remain. No performance acceptance is claimed.

Raw measurements, drivers, all variant source files and test logs are in the
archive with a per-file SHA256 manifest. The first zero-variant CLI build
failed with undefined-type diagnostics; an identical-source serial retry
passed. Both logs are retained; the cause is unproven. No source fix or
performance result is inferred from that transient build failure.
