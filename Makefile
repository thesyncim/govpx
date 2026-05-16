SHELL := /bin/sh

GO ?= go
GOFMT ?= gofmt
GIT ?= git
CURL ?= curl
AWK ?= awk
GOTOOLCHAIN ?= go1.26.3

GOCACHE ?= $(CURDIR)/.gocache
CORACLE_BUILD := internal/coracle/build
LIBVPX_TEST_DATA_BASE := https://storage.googleapis.com/downloads.webmproject.org/test_data/libvpx
LIBVPX_TEST_DATA_MK := $(CORACLE_BUILD)/libvpx-v1.16.0/test/test-data.mk
ORACLE := $(CORACLE_BUILD)/govpx-vpx-oracle
VPXENC := $(CORACLE_BUILD)/vpxenc
VPXENC_ORACLE := $(CORACLE_BUILD)/vpxenc-oracle
VPXENC_FRAMEFLAGS := $(CORACLE_BUILD)/vpxenc-frameflags
VPXDEC := $(CORACLE_BUILD)/vpxdec
VPXDEC_VP9 := $(CORACLE_BUILD)/vpxdec-vp9
VPXENC_VP9 := $(CORACLE_BUILD)/vpxenc-vp9
VPXENC_VP9_FRAMEFLAGS := $(CORACLE_BUILD)/vpxenc-vp9-frameflags
VPX_TEMPORAL_SVC_ENCODER := $(CORACLE_BUILD)/vpx_temporal_svc_encoder
VP8_TEST_DATA_DIR := $(CORACLE_BUILD)/test-data/vp8
VP9_TEST_DATA_DIR := $(CORACLE_BUILD)/test-data/vp9
VP8_ENCODER_SOURCE_DIR := $(CORACLE_BUILD)/test-data/encoder
PGO_PROFILE := cmd/govpx-bench/default.pgo
PGO_FINGERPRINT := cmd/govpx-bench/default.pgo.sources.sha256

# The pinned libvpx manifest currently lists 62 non-invalid vp80*.ivf names;
# four segmentation fixtures carry I420 IVF FourCCs, so the VP8 decoder
# conformance subset is 58 VP80 IVF vectors.
VP8_DECODER_IVF_MIN ?= 58
VP8_INVALID_IVF_MIN ?= 2
VP9_DECODER_IVF_MIN ?= 7
VP9_INVALID_IVF_MIN ?= 17
VP9_DECODER_PROFILE0_WEBM_MIN ?= 101
VP9_DECODER_PROFILE_WEBM_MIN ?= 11
VP8_ENCODER_SOURCE_MIN ?= 2
VP8_ENCODER_SOURCE_FRAMES ?= 6
VP8_ENCODER_SOURCE_FILES ?= park_joy_90p_8_420.y4m desktopqvga.320_240.yuv
VP9_DECODER_PROFILE0_WEBM_QUANTIZER_FILES := $(shell $(AWK) 'BEGIN { for (i = 0; i < 64; i++) printf "vp90-2-00-quantizer-%02d.webm ", i }')
VP9_DECODER_PROFILE0_WEBM_FILES ?= \
	$(VP9_DECODER_PROFILE0_WEBM_QUANTIZER_FILES) \
	vp90-2-01-sharpness-1.webm \
	vp90-2-01-sharpness-2.webm \
	vp90-2-01-sharpness-3.webm \
	vp90-2-01-sharpness-4.webm \
	vp90-2-01-sharpness-5.webm \
	vp90-2-01-sharpness-6.webm \
	vp90-2-01-sharpness-7.webm \
	vp90-2-02-size-08x08.webm \
	vp90-2-02-size-08x10.webm \
	vp90-2-02-size-10x08.webm \
	vp90-2-02-size-16x16.webm \
	vp90-2-02-size-16x18.webm \
	vp90-2-02-size-18x16.webm \
	vp90-2-02-size-32x32.webm \
	vp90-2-02-size-32x34.webm \
	vp90-2-02-size-34x32.webm \
	vp90-2-02-size-64x64.webm \
	vp90-2-02-size-64x66.webm \
	vp90-2-02-size-66x64.webm \
	vp90-2-02-size-130x132.webm \
	vp90-2-02-size-132x130.webm \
	vp90-2-02-size-180x180.webm \
	vp90-2-03-deltaq.webm \
	vp90-2-06-bilinear.webm \
	vp90-2-07-frame_parallel.webm \
	vp90-2-08-tile_1x4.webm \
	vp90-2-08-tile_1x8.webm \
	vp90-2-08-tile_1x2_frame_parallel.webm \
	vp90-2-09-aq2.webm \
	vp90-2-09-lf_deltas.webm \
	vp90-2-10-show-existing-frame.webm \
	vp90-2-11-size-351x287.webm \
	vp90-2-14-resize-10frames-fp-tiles-1-2.webm \
	vp90-2-14-resize-10frames-fp-tiles-1-4.webm \
	vp90-2-15-segkey.webm \
	vp90-2-16-intra-only.webm \
	vp90-2-19-skip.webm

VP9_DSP_ORACLE_BIN := $(CORACLE_BUILD)/govpx-vp9-dsp-oracle
VP9_DSP_TESTDATA := internal/vp9/dsp/testdata/dsp_oracle.bin

.PHONY: all ci fmtcheck test test-purego vp9-decoder-conformance pgo-refresh pgo-update-fingerprint pgo-check verify verify-production verify-decoder-parity verify-bd-rate verify-quality oracle-test byte-parity fuzz-controls fuzz-rename decoder-oracle-test oracle-tools vp9-vpxdec-tools fetch-test-data fetch-vp8-test-data fetch-vp9-test-data fetch-encoder-test-data scoreboard scoreboard-update vp9-dsp-oracle

all: ci

ci: fmtcheck pgo-check test test-purego vp9-decoder-conformance

fmtcheck:
	files="$$($(GOFMT) -l $$($(GIT) ls-files '*.go'))"; \
	if [ -n "$$files" ]; then \
		printf 'gofmt needed:\n%s\n' "$$files"; \
		exit 1; \
	fi
	hashseeds="$$(find testdata/fuzz -mindepth 2 -maxdepth 2 -type f -regex '.*/[0-9a-f]\{16\}' 2>/dev/null)"; \
	if [ -n "$$hashseeds" ]; then \
		printf 'hash-named fuzz seeds detected (run: make fuzz-rename):\n%s\n' "$$hashseeds"; \
		exit 1; \
	fi

# fuzz-rename walks testdata/fuzz/<FuzzName>/ and renames any seed
# whose filename is the default 16-hex SHA to regression_<case>_<hash8>
# via `git mv`. Idempotent: rerun after every `make fuzz-controls`
# session. The `fmtcheck` gate fails if hash-named seeds remain, so
# this also unblocks commits after a fuzz discovery.
fuzz-rename:
	GOCACHE="$(GOCACHE)" GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) run ./cmd/govpx-fuzz-rename

verify: ci

# verify-quality runs the govpx-bench VP9 quality-gate fixture suite
# (panning 360p / 2 Mbps + checker 360p / 600 kbps) against the pinned
# libvpx vpxenc-vp9 reference. The bench exits non-zero when govpx
# PSNR drops below 20 dB, SSIM below 0.70, or trails libvpx by more
# than 2 dB PSNR / 0.03 SSIM. Tune those thresholds via -quality-min-psnr
# etc. on the bench CLI when investigating regressions.
verify-quality: vp9-vpxdec-tools
	GOCACHE="$(GOCACHE)" GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) build -o $(CORACLE_BUILD)/govpx-bench ./cmd/govpx-bench
	$(CORACLE_BUILD)/govpx-bench -quality-fixtures -quality-gate -libvpx-vpxenc-vp9="$(VPXENC_VP9)" -auto-libvpx=false

verify-production: ci oracle-test byte-parity scoreboard

verify-decoder-parity: ci decoder-oracle-test

# verify-bd-rate runs the slow per-feature VP9 BD-rate quality gates
# under cmd/govpx-bench/benchcmd. The default short test mode skips
# these because each measurement takes ~5-15s and the full sweep
# adds ~30s. Run this target before merging any change that touches
# AltRef, ARNR, TPL, AltRefAQ, or VP9 AQ-mode code paths.
#
# The target also captures the absolute govpx-vs-libvpx BD-rate
# reference by driving the libvpx vpxenc-vp9-frameflags helper with
# matching feature flags; the per-feature scoreboard logged at the
# end of the run carries `govpx BD-rate | libvpx BD-rate |
# govpx-vs-libvpx` columns so the absolute gap to libvpx is visible
# without instrumenting the gate tests. GOVPX_BD_RATE_BUILD_LIBVPX=1
# triggers a one-shot libvpx build when the helper binary is
# missing; GOVPX_BD_RATE_LIBVPX_REQUIRED=1 elevates the libvpx
# assertion from a soft-skip to t.Fatal so CI fails fast when the
# oracle is unavailable.
verify-bd-rate: $(VPXENC_VP9_FRAMEFLAGS)
	GOCACHE="$(GOCACHE)" GOTOOLCHAIN="$(GOTOOLCHAIN)" \
		GOVPX_BD_RATE_GATES=1 \
		GOVPX_BD_RATE_BUILD_LIBVPX=1 \
		GOVPX_BD_RATE_LIBVPX_REQUIRED=1 \
		GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN="$(VPXENC_VP9_FRAMEFLAGS)" \
		$(GO) test -count=1 -v -run 'TestVP9FeatureBDRate' -timeout 600s . ./cmd/govpx-bench/benchcmd/

$(VPXENC_VP9_FRAMEFLAGS):
	internal/coracle/build_vpxenc_vp9_frameflags.sh >/dev/null

# vp9-dsp-oracle rebuilds the VP9-decoder-only libvpx variant + the
# DSP oracle binary, then regenerates the committed testdata corpus.
# Run this when libvpx is updated or vp9_dsp_oracle.c changes.
vp9-dsp-oracle:
	internal/coracle/build_libvpx_vp9.sh >/dev/null
	"$(VP9_DSP_ORACLE_BIN)" > "$(VP9_DSP_TESTDATA).tmp"
	mv "$(VP9_DSP_TESTDATA).tmp" "$(VP9_DSP_TESTDATA)"
	printf 'wrote %s\n' "$(VP9_DSP_TESTDATA)"

test:
	GOCACHE="$(GOCACHE)" GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) test ./... -count=1

test-purego:
	sfiles="$$(GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) list -tags purego -f '{{if .SFiles}}{{.ImportPath}} {{.SFiles}}{{end}}' ./...)"; \
	if [ -n "$$sfiles" ]; then \
		printf 'purego selected assembly files:\n%s\n' "$$sfiles"; \
		exit 1; \
	fi
	GOCACHE="$(GOCACHE)" GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) test -tags purego ./... -count=1

vp9-decoder-conformance: vp9-vpxdec-tools fetch-vp9-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_VPXDEC_VP9_BIN="$(VPXDEC_VP9)" \
	GOVPX_VP9_TEST_DATA_PATH="$(VP9_TEST_DATA_DIR)" \
	GOVPX_VP9_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_TEST_DATA_MIN="$(VP9_DECODER_IVF_MIN)" \
	GOVPX_VP9_TEST_DATA_STRICT=1 \
	GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN="$(VP9_DECODER_PROFILE0_WEBM_MIN)" \
	GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_PROFILE_TEST_DATA_MIN="$(VP9_DECODER_PROFILE_WEBM_MIN)" \
	GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_INVALID_TEST_DATA_MIN="$(VP9_INVALID_IVF_MIN)" \
	$(GO) test . -run 'TestVP9Decoder(Official(IVFTestDataMatchesLibvpx|IVFTestDataThreadedMatchesSerial|Profile0WebMTestDataMatchesLibvpx|ProfileWebMTestDataReturnsUnsupported|InvalidIVFTestDataRejectedLikeLibvpx)|ThreadingOfficial(IVFMatchesSerial|Profile0WebMMatchesSerial|Profile0TileColumnsUseWorkers))$$' -count=1 -timeout 10m

pgo-refresh:
	mkdir -p .pgo
	GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) build -pgo=off -o .pgo/govpx-bench-pgo ./cmd/govpx-bench
	./.pgo/govpx-bench-pgo -width=1920 -height=1080 -frames=180 -fps=30 -bitrate=4000 -mode=realtime -cpu-used=8 -encode-only -cpuprofile=.pgo/encode.pgo >/dev/null
	./.pgo/govpx-bench-pgo -width=1280 -height=720 -frames=240 -fps=30 -bitrate=2500 -mode=realtime -cpu-used=8 -cpuprofile=.pgo/quality.pgo >/dev/null
	GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) tool pprof -proto .pgo/encode.pgo .pgo/quality.pgo > "$(PGO_PROFILE).tmp"
	mv "$(PGO_PROFILE).tmp" "$(PGO_PROFILE)"
	rm -rf .pgo
	$(MAKE) pgo-update-fingerprint

pgo-update-fingerprint:
	scripts/pgo-fingerprint.sh > "$(PGO_FINGERPRINT).tmp"
	mv "$(PGO_FINGERPRINT).tmp" "$(PGO_FINGERPRINT)"

pgo-check:
	test -s "$(PGO_PROFILE)"
	test -s "$(PGO_FINGERPRINT)"
	actual="$$(scripts/pgo-fingerprint.sh)"; \
	expected="$$(cat "$(PGO_FINGERPRINT)")"; \
	if [ "$$actual" != "$$expected" ]; then \
		printf '%s\n' "PGO profile is out of sync with VP8 benchmark hot-path sources."; \
		printf '%s\n' "Run: make pgo-refresh"; \
		printf 'expected %s\nactual   %s\n' "$$expected" "$$actual"; \
		exit 1; \
	fi
	GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) build -pgo="$(PGO_PROFILE)" -o /tmp/govpx-bench-pgo-check ./cmd/govpx-bench
	rm -f /tmp/govpx-bench-pgo-check

oracle-test: oracle-tools vp9-vpxdec-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXDEC_VP9_BIN="$(VPXDEC_VP9)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPXENC_FRAMEFLAGS="$(VPXENC_FRAMEFLAGS)" \
	GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN="$(VPXENC_VP9_FRAMEFLAGS)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_TEST_DATA_REQUIRED=1 \
	GOVPX_TEST_DATA_MIN="$(VP8_DECODER_IVF_MIN)" \
	GOVPX_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_INVALID_TEST_DATA_MIN="$(VP8_INVALID_IVF_MIN)" \
	GOVPX_VP9_TEST_DATA_PATH="$(VP9_TEST_DATA_DIR)" \
	GOVPX_VP9_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_TEST_DATA_MIN="$(VP9_DECODER_IVF_MIN)" \
	GOVPX_VP9_TEST_DATA_STRICT=1 \
	GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN="$(VP9_DECODER_PROFILE0_WEBM_MIN)" \
	GOVPX_VP9_PROFILE_TEST_DATA_MIN="$(VP9_DECODER_PROFILE_WEBM_MIN)" \
	GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_INVALID_TEST_DATA_MIN="$(VP9_INVALID_IVF_MIN)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	GOVPX_ENCODER_TEST_DATA_REQUIRED=1 \
	GOVPX_ENCODER_TEST_DATA_MIN="$(VP8_ENCODER_SOURCE_MIN)" \
	GOVPX_ENCODER_TEST_DATA_FRAMES="$(VP8_ENCODER_SOURCE_FRAMES)" \
	$(GO) test . -run 'Test(Oracle|VP9EncoderVpxdecOracleAccepts|VP9DecoderVpxdecOracleMatches|VP9DecoderOfficial|VP9DecoderThreadingOfficial)' -count=1 -timeout 10m

SCOREBOARD_TESTS := TestOracleReconstructionAdler32Match|TestOracleRecodeRowParity|TestOracleARNRBufferAdler|TestOracleEncoderQHistogramScoreboard|TestOracleInterDecisionMatchRate|TestOracleSplitMVDecisionMatchRate|TestOracleEncoderTraceInterCandidateScoreboard|TestOracle128x128InterQDriftScoreboard|TestOracleLoopFilterHeaderMatchRate|TestOracleSecondPassAllocationCompare|TestOracleChromaSubpelScoreboard|TestOracleImprovedMVScoreboard|TestOracleCBRDropFrameScoreboard|TestOracleCandidateRateScoreboard|TestOracleInterModeDistributionScoreboard|TestOracleTemporalSVCParity|TestVP9OracleRuntimeControl(ByteParityScoreboard|ConstantByteParityMatrix)
BYTE_PARITY_TESTS := Test(OracleEncoder(StreamByteParity|CopyReferenceFrameParity|QuantizerMetadataParity|ProductionRuntimeTransitions720p)|VP9EncoderVpxencOracle(Checker320KeyframeByteParity|Stepped320FixedQuantizerKeyframeByteParity|CBRKeyframeByteParity)|VP9Oracle(CopyReferenceFrameStrictParity|SelectedStreamByteParityGate|PinnedRuntimeControlByteParity|Threaded720pStrictByteParityUsesTileWriter|PinnedNewModeStrictByteParity|InvisibleKeyFrameStrictByteParity|EncoderStreamByteParity(FrameFlagsMatrix|ControlCrossMatrix|LookaheadFlushBursts)))|FuzzOracleEncoderRuntimeControlTransitions
FUZZTIME ?= 30s
FUZZPARALLEL ?= 1

byte-parity: oracle-tools vp9-vpxdec-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPXENC_FRAMEFLAGS="$(VPXENC_FRAMEFLAGS)" \
	GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN="$(VPXENC_VP9_FRAMEFLAGS)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	$(GO) test -tags govpx_oracle_trace . -run '$(BYTE_PARITY_TESTS)' -count=1 -timeout 15m

fuzz-controls: oracle-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPXENC_FRAMEFLAGS="$(VPXENC_FRAMEFLAGS)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	$(GO) test -tags govpx_oracle_trace . -run '^$$' -fuzz '^FuzzOracleEncoderRuntimeControlTransitions$$' -fuzztime '$(FUZZTIME)' -parallel '$(FUZZPARALLEL)' -timeout 30m

scoreboard: oracle-tools vp9-vpxdec-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN="$(VPXENC_VP9_FRAMEFLAGS)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	$(GO) run ./cmd/scoreboard-report -- -tags govpx_oracle_trace . -run '$(SCOREBOARD_TESTS)' -count=1 -timeout 10m

scoreboard-update: oracle-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_UPDATE_BASELINES=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	$(GO) run ./cmd/scoreboard-report -- -tags govpx_oracle_trace . -run '$(SCOREBOARD_TESTS)' -count=1 -timeout 10m

decoder-oracle-test: oracle-tools vp9-vpxdec-tools fetch-vp8-test-data fetch-vp9-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXDEC_VP9_BIN="$(VPXDEC_VP9)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_TEST_DATA_REQUIRED=1 \
	GOVPX_TEST_DATA_MIN="$(VP8_DECODER_IVF_MIN)" \
	GOVPX_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_INVALID_TEST_DATA_MIN="$(VP8_INVALID_IVF_MIN)" \
	GOVPX_VP9_TEST_DATA_PATH="$(VP9_TEST_DATA_DIR)" \
	GOVPX_VP9_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_TEST_DATA_MIN="$(VP9_DECODER_IVF_MIN)" \
	GOVPX_VP9_TEST_DATA_STRICT=1 \
	GOVPX_VP9_PROFILE0_WEBM_TEST_DATA_MIN="$(VP9_DECODER_PROFILE0_WEBM_MIN)" \
	GOVPX_VP9_PROFILE_TEST_DATA_MIN="$(VP9_DECODER_PROFILE_WEBM_MIN)" \
	GOVPX_VP9_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_VP9_INVALID_TEST_DATA_MIN="$(VP9_INVALID_IVF_MIN)" \
	$(GO) test . -run 'Test(Oracle(Libvpx(ExtendedDecodeModesAvailable|DecoderReferenceControls|ErrorConcealment.*|KeyFrameResolutionChange|PostProcess.*)|ExternalIVFTestData(MatchesLibvpx|DecodeIntoMatchesLibvpx)|ExternalInvalidIVFTestDataRejectedLikeLibvpx|GeneratedLibvpxCorpusMatchesLibvpx)|VP9Decoder(VpxdecOracleMatches.*|Official.*|ThreadingOfficial.*))$$' -count=1 -timeout 10m

oracle-tools: $(ORACLE)
	internal/coracle/build_vpxenc.sh >/dev/null
	sh internal/coracle/build_vpxenc_oracle.sh >/dev/null
	sh internal/coracle/build_vpxenc_frameflags.sh >/dev/null
	test -x "$(VPXENC)"
	test -x "$(VPXENC_ORACLE)"
	test -x "$(VPXENC_FRAMEFLAGS)"
	test -x "$(VPXDEC)"
	test -x "$(VPX_TEMPORAL_SVC_ENCODER)"

vp9-vpxdec-tools:
	internal/coracle/build_vpxdec_vp9.sh >/dev/null
	sh internal/coracle/build_vpxenc_vp9_frameflags.sh >/dev/null
	test -x "$(VPXDEC_VP9)"
	test -x "$(VPXENC_VP9)"
	test -x "$(VPXENC_VP9_FRAMEFLAGS)"

$(ORACLE): internal/coracle/build_libvpx.sh internal/coracle/vpx_oracle.c
	internal/coracle/build_libvpx.sh >/dev/null

fetch-test-data: fetch-vp8-test-data fetch-vp9-test-data fetch-encoder-test-data

fetch-vp8-test-data: $(ORACLE)
	mkdir -p "$(VP8_TEST_DATA_DIR)"
	$(AWK) '/LIBVPX_TEST_DATA-\$$\(CONFIG_VP8_DECODER\)/ && $$NF ~ /vp80.*\.ivf$$/ {print $$NF}' "$(LIBVPX_TEST_DATA_MK)" | sort | while read f; do \
		if [ ! -s "$(VP8_TEST_DATA_DIR)/$$f" ]; then \
			printf 'fetch %s\n' "$$f"; \
			$(CURL) -fsSL --retry 3 -o "$(VP8_TEST_DATA_DIR)/$$f.tmp" "$(LIBVPX_TEST_DATA_BASE)/$$f"; \
			mv "$(VP8_TEST_DATA_DIR)/$$f.tmp" "$(VP8_TEST_DATA_DIR)/$$f"; \
		fi; \
	done

fetch-vp9-test-data: $(ORACLE)
	mkdir -p "$(VP9_TEST_DATA_DIR)"
	$(AWK) '/LIBVPX_TEST_DATA-\$$\(CONFIG_VP9_DECODER\)/ && ($$NF ~ /^(invalid-)?vp9[0-3].*\.ivf$$/ || $$NF ~ /^vp9[1-3].*\.webm$$/) {print $$NF}' "$(LIBVPX_TEST_DATA_MK)" | sort | while read f; do \
		if [ ! -s "$(VP9_TEST_DATA_DIR)/$$f" ]; then \
			printf 'fetch %s\n' "$$f"; \
			$(CURL) -fsSL --retry 3 -o "$(VP9_TEST_DATA_DIR)/$$f.tmp" "$(LIBVPX_TEST_DATA_BASE)/$$f"; \
			mv "$(VP9_TEST_DATA_DIR)/$$f.tmp" "$(VP9_TEST_DATA_DIR)/$$f"; \
		fi; \
	done
	for f in $(VP9_DECODER_PROFILE0_WEBM_FILES); do \
		if [ ! -s "$(VP9_TEST_DATA_DIR)/$$f" ]; then \
			printf 'fetch %s\n' "$$f"; \
			$(CURL) -fsSL --retry 3 -o "$(VP9_TEST_DATA_DIR)/$$f.tmp" "$(LIBVPX_TEST_DATA_BASE)/$$f"; \
			mv "$(VP9_TEST_DATA_DIR)/$$f.tmp" "$(VP9_TEST_DATA_DIR)/$$f"; \
		fi; \
	done

fetch-encoder-test-data: oracle-tools
	mkdir -p "$(VP8_ENCODER_SOURCE_DIR)"
	for f in $(VP8_ENCODER_SOURCE_FILES); do \
		if [ ! -s "$(VP8_ENCODER_SOURCE_DIR)/$$f" ]; then \
			printf 'fetch %s\n' "$$f"; \
			$(CURL) -fsSL --retry 3 -o "$(VP8_ENCODER_SOURCE_DIR)/$$f.tmp" "$(LIBVPX_TEST_DATA_BASE)/$$f"; \
			mv "$(VP8_ENCODER_SOURCE_DIR)/$$f.tmp" "$(VP8_ENCODER_SOURCE_DIR)/$$f"; \
		fi; \
	done
