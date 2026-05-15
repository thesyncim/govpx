# VP8 Loop-Filter Performance Levers

Date: 2026-05-15

Constraint: preserve libvpx VP8 decisions. Do not land heuristic shortcuts that
change loop-filter level selection, motion-search candidates, or oracle bytes.

## Current Measurement

Commands used after `aaa8120`:

```bash
GOTOOLCHAIN=go1.26.3 go run ./cmd/govpx-bench -width 1280 -height 720 -frames 120 -fps 30 -bitrate 1500 -mode realtime -cpu-used=-4 -auto-libvpx=false -encode-only -phase-timing
GOTOOLCHAIN=go1.26.3 go run ./cmd/govpx-bench -width 1920 -height 1080 -frames 120 -fps 30 -bitrate 8000 -mode good -auto-libvpx=false -encode-only -phase-timing
```

Results:

| config | ns/frame | inter_recon | lf_pick | lf_apply | packet |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1280x720 realtime cpu=-4 | 9.52 ms | 6.63 ms | 1.80 ms | 218.10 us | 510.68 us |
| 1920x1080 good | 13.70 ms | 8.64 ms | 328.30 us | 1.11 ms | 1.92 ms |

The encoded output bytes, keyframe bytes, interframe bytes, and motion-search
counters matched the pre-change runs for these fixtures.

## Lever Review

| lever | status | decision |
| --- | --- | --- |
| 1a. Make `pickFull` use partial-frame trials | Rejected | Not libvpx-equivalent. VP8 `vp8cx_pick_filter_level` copies full luma, runs `vp8_loop_filter_frame_yonly`, and computes full-frame SSE. Only `vp8cx_pick_filter_level_fast` uses partial-frame copy/filter/SSE. |
| 1b. Cap full-picker trials to four | Rejected | Changes libvpx's binary-search loop and tie behavior. This is a heuristic, not a port. |
| 1c. Use `pickFast` at realtime speed 4 | Rejected | libvpx VP8 speed features set `auto_filter=1` at speed 4 and `auto_filter=0` only above 4. Existing tests pin this. |
| 2. Fuse luma H/V loop-filter edges | ARM64: not a libvpx port | libvpx ARM `vp8_loop_filter_bv_neon` and `vp8_loop_filter_bh_neon` call the three luma inner-edge kernels sequentially. A fused ARM64 kernel would be new work and must preserve edge dependencies. x86 has fused luma SSE2 wrappers and remains a valid amd64 port target. |
| 3. Trim denoise picker overhead | Still valid | Realtime profile shows the denoise picker is dominated by exact NEWMV search and sub-pel variance. Safe work is kernel/loop overhead only, not candidate pruning. |
| 4. Boolean writer renorm | Done | `2a6ea25` replaced byte-table bool renorm with `bits.LeadingZeros8`, preserving exact bitstream output. |
| 5. `extendPlane` border copy | Partially done | `aaa8120` replaced fixed 16/32 byte row-border memmove fills with direct 64-bit stores, preserving exact extended pixels. Do not skip top/bottom extension unless a libvpx-equivalent condition is proven. |

## Next Exact Targets

1. x86 loop-filter luma wrappers: port libvpx `vp8_loop_filter_bv_y_sse2` and
   `vp8_loop_filter_bh_y_sse2` for amd64. This is the cleanest table item that
   is actually present in libvpx as fused luma work.
2. ARM64 luma loop-filter edge kernel tuning: any fused ARM64 luma kernel is
   non-upstream work. It can still be exact, but it must apply edge 4, then edge
   8 using the updated columns/rows, then edge 12 using the updated data.
3. Denoise SIMD: port libvpx ARM NEON and x86 SSE2 denoiser filters. This only
   matters when `NoiseSensitivity > 0`, but that is the active realtime path in
   the benchmark profile.
4. Sub-pel variance and search-loop overhead: continue exact kernel work. The
   current path is variance dominated; early NEWMV rejection is off-limits under
   the oracle constraint.
