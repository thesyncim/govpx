# VP9 Parity Agent Guidelines

- Scope is VP9 only. Do not change VP8 behavior unless it is mechanically required by shared infrastructure and covered by existing tests.
- Do not touch session manager, client, example desktop, or unrelated application files.
- Preserve zero hot-path allocations. Verify allocation-sensitive encode, decode, motion search, rate control, tokenization, and trace-guard paths when changing them.
- Keep stack, inlining, and small-integer discipline tight: prefer fixed-size stack data, avoid interface escapes, avoid unnecessary slices, maps, and closures, and keep arithmetic types close to libvpx semantics.
- Do not add heuristic parity fixes. Every parity change must be justified against libvpx behavior, oracle output, bitstream semantics, or an explicit VP9 spec requirement.
- Update public docs when API behavior, exported types, options, or user-visible controls change.
- Oracle and trace code must be zero-cost unless built with `govpx_oracle_trace`; keep guards compile-time where possible and prevent oracle-only state from entering normal builds.
- Work in substantial safe points: make coherent chunks that build, test, and explain one parity step rather than broad mixed changes.
- After each verified chunk, commit and push. Include the verification commands and results in the commit or PR notes so later agents can resume safely.
