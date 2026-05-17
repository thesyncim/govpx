# VP8 controls + parity audit tracker

Source: VP8 gap audit, started 2026-05-17. Branch: `vp8-encoder-decoder-controls`.

## Items

| #  | Item                                                                   | Status |
|----|------------------------------------------------------------------------|--------|
| 1  | Decoder GETs: FrameCorrupted, LastReferenceUpdates, LastReferencesUsed | ✅      |
| 2  | Encoder runtime SetAutoAltRef                                          | ✅      |
| 3  | VP8E_SET_SCALEMODE / spatial resampler                                 | ⬜      |
| 4  | Burn down ~24 deferred VP8 fuzz seeds                                  | ⬜      |
| 5  | ALT_LF segmentation                                                    | ⬜      |
| 6  | CBR golden-frame correction-factor branch                              | ⬜      |
| 7  | Cyclic-refresh + static-background segmentation parity                 | ⬜      |
| 8  | SPLITMV label-level RD oracle + improved-MV comparator                 | ⬜      |
| 9  | Right-edge chroma sub-pel residual (96x96 / 128x128)                   | ⬜      |
| 10 | VP8 version 1-3 decoder spec corners                                   | ⬜      |
| 11 | VP8D_SET_DECRYPTOR (encrypted bitstream)                               | ⬜      |

⬜ open  ⏳ in flight  ✅ landed

## Sync log

- 2026-05-17 worktree branched from origin/main `4219a4c`.
