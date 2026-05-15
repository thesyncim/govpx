# govpx tracker

Reference: libvpx v1.16.0. VP9 scope is documented in [UPSTREAM.md](UPSTREAM.md).

## Gates

- Full CI gate: `make ci`
- Full parity/production gate: `make verify-production`
- Decoder-only proof gate: `make verify-decoder-parity`
- Focused work should add or extend oracle coverage before claiming support.
- Correctness and libvpx quality parity come before performance.
- Encoder parity means libvpx-equivalent visible quality, rate behavior,
  reference policy, mode/MV choices, and residual decisions. Do not chase
  byte-for-byte identity in paths that do not affect quality, decoder-visible
  output, future encoder decisions, or oracle diagnosis.
- Treat "100% parity" as quality/rate and quality-relevant decision
  equivalence, not universal bit-exactness.
- Future agent handoffs should state that percentages are quality-equivalence
  estimates. Bit exactness is a tool for proving important paths, not the
  product target by itself.
- Bit-exact output is still required where deterministic paths make it the
  right proof, especially packet validity, frame headers, reference
  refresh/copy/sign-bias bits, decoder MD5s, and low-level entropy writers.
- Scoreboard baselines are regression gates and diagnostic coverage, not closed
  byte-parity proofs. A green scoreboard means a measured gap stayed within its
  pinned baseline; byte-parity claims need explicit strict oracle tests.
- This is still pre-release encoder work: internal helper signatures should
  follow the current parity model directly. Do not carry legacy compatibility
  wrappers for older internal call shapes.
- Every safe point should end with `make verify-production` and
  `git status --short`.

## VP9 Scope

Authoritative scope lives in [UPSTREAM.md](UPSTREAM.md). VP9 support is full
Profile 0 only; no profiles 1-3, alpha, high-bit-depth/deep-color, or non-4:2:0
variants. RTP/WebRTC payload compatibility is in scope for both VP8 and VP9.
Assembly/SIMD optimization is deferred until full VP9 encoder parity.
