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
VPX_TEMPORAL_SVC_ENCODER := $(CORACLE_BUILD)/vpx_temporal_svc_encoder
VP8_TEST_DATA_DIR := $(CORACLE_BUILD)/test-data/vp8
VP8_ENCODER_SOURCE_DIR := $(CORACLE_BUILD)/test-data/encoder
PGO_PROFILE := cmd/govpx-bench/default.pgo

# The pinned libvpx manifest currently lists 62 non-invalid vp80*.ivf names;
# four segmentation fixtures carry I420 IVF FourCCs, so the VP8 decoder
# conformance subset is 58 VP80 IVF vectors.
VP8_DECODER_IVF_MIN ?= 58
VP8_INVALID_IVF_MIN ?= 2
VP8_ENCODER_SOURCE_MIN ?= 2
VP8_ENCODER_SOURCE_FRAMES ?= 6
VP8_ENCODER_SOURCE_FILES ?= park_joy_90p_8_420.y4m desktopqvga.320_240.yuv

VP9_DSP_ORACLE_BIN := $(CORACLE_BUILD)/govpx-vp9-dsp-oracle
VP9_DSP_TESTDATA := internal/vp9/dsp/testdata/dsp_oracle.bin

.PHONY: all ci fmtcheck test test-purego pgo-refresh verify verify-production verify-decoder-parity oracle-test decoder-oracle-test oracle-tools vp9-vpxdec-tools fetch-test-data fetch-vp8-test-data fetch-encoder-test-data scoreboard scoreboard-update vp9-dsp-oracle

all: ci

ci: fmtcheck test test-purego

fmtcheck:
	files="$$($(GOFMT) -l $$($(GIT) ls-files '*.go'))"; \
	if [ -n "$$files" ]; then \
		printf 'gofmt needed:\n%s\n' "$$files"; \
		exit 1; \
	fi

verify: ci

verify-production: ci oracle-test

verify-decoder-parity: ci decoder-oracle-test

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

pgo-refresh:
	mkdir -p .pgo
	GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) build -pgo=off -o .pgo/govpx-bench-pgo ./cmd/govpx-bench
	./.pgo/govpx-bench-pgo -width=1920 -height=1080 -frames=180 -fps=30 -bitrate=4000 -mode=realtime -cpu-used=8 -encode-only -cpuprofile=.pgo/encode.pgo >/dev/null
	./.pgo/govpx-bench-pgo -width=1280 -height=720 -frames=240 -fps=30 -bitrate=2500 -mode=realtime -cpu-used=8 -cpuprofile=.pgo/quality.pgo >/dev/null
	GOTOOLCHAIN="$(GOTOOLCHAIN)" $(GO) tool pprof -proto .pgo/encode.pgo .pgo/quality.pgo > "$(PGO_PROFILE).tmp"
	mv "$(PGO_PROFILE).tmp" "$(PGO_PROFILE)"
	rm -rf .pgo

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
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_TEST_DATA_REQUIRED=1 \
	GOVPX_TEST_DATA_MIN="$(VP8_DECODER_IVF_MIN)" \
	GOVPX_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_INVALID_TEST_DATA_MIN="$(VP8_INVALID_IVF_MIN)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	GOVPX_ENCODER_TEST_DATA_REQUIRED=1 \
	GOVPX_ENCODER_TEST_DATA_MIN="$(VP8_ENCODER_SOURCE_MIN)" \
	GOVPX_ENCODER_TEST_DATA_FRAMES="$(VP8_ENCODER_SOURCE_FRAMES)" \
	$(GO) test . -run 'Test(Oracle|VP9EncoderVpxdecOracleAccepts|VP9DecoderVpxdecOracleMatches)' -count=1 -timeout 10m

SCOREBOARD_TESTS := TestOracleReconstructionAdler32Match|TestOracleRecodeRowParity|TestOracleARNRBufferAdler|TestOracleEncoderQHistogramScoreboard|TestOracleInterDecisionMatchRate|TestOracleSplitMVDecisionMatchRate|TestOracleEncoderTraceInterCandidateScoreboard|TestOracle128x128InterQDriftScoreboard|TestOracleLoopFilterHeaderMatchRate|TestOracleSecondPassAllocationCompare|TestOracleChromaSubpelScoreboard|TestOracleImprovedMVScoreboard|TestOracleCBRDropFrameScoreboard|TestOracleCandidateRateScoreboard|TestOracleInterModeDistributionScoreboard|TestOracleTemporalSVCParity

scoreboard: oracle-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_VPXENC_ORACLE="$(VPXENC_ORACLE)" \
	GOVPX_VPX_TEMPORAL_SVC_ENCODER="$(VPX_TEMPORAL_SVC_ENCODER)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	$(GO) run ./cmd/scoreboard-report -- . -run '$(SCOREBOARD_TESTS)' -count=1 -timeout 10m

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
	$(GO) run ./cmd/scoreboard-report -- . -run '$(SCOREBOARD_TESTS)' -count=1 -timeout 10m

decoder-oracle-test: oracle-tools fetch-vp8-test-data
	GOCACHE="$(GOCACHE)" \
	GOTOOLCHAIN="$(GOTOOLCHAIN)" \
	GOVPX_WITH_ORACLE=1 \
	GOVPX_ORACLE="$(ORACLE)" \
	GOVPX_VPXDEC="$(VPXDEC)" \
	GOVPX_VPXENC="$(VPXENC)" \
	GOVPX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOVPX_TEST_DATA_REQUIRED=1 \
	GOVPX_TEST_DATA_MIN="$(VP8_DECODER_IVF_MIN)" \
	GOVPX_INVALID_TEST_DATA_REQUIRED=1 \
	GOVPX_INVALID_TEST_DATA_MIN="$(VP8_INVALID_IVF_MIN)" \
	$(GO) test . -run 'TestOracle(Libvpx(ExtendedDecodeModesAvailable|ErrorConcealment.*|KeyFrameResolutionChange|PostProcess.*)|ExternalIVFTestData(MatchesLibvpx|DecodeIntoMatchesLibvpx)|ExternalInvalidIVFTestDataRejectedLikeLibvpx|GeneratedLibvpxCorpusMatchesLibvpx)$$' -count=1 -timeout 10m

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
	test -x "$(VPXDEC_VP9)"
	test -x "$(VPXENC_VP9)"

$(ORACLE): internal/coracle/build_libvpx.sh internal/coracle/vpx_oracle.c
	internal/coracle/build_libvpx.sh >/dev/null

fetch-test-data: fetch-vp8-test-data fetch-encoder-test-data

fetch-vp8-test-data: $(ORACLE)
	mkdir -p "$(VP8_TEST_DATA_DIR)"
	$(AWK) '/LIBVPX_TEST_DATA-\$$\(CONFIG_VP8_DECODER\)/ && $$NF ~ /vp80.*\.ivf$$/ {print $$NF}' "$(LIBVPX_TEST_DATA_MK)" | sort | while read f; do \
		if [ ! -s "$(VP8_TEST_DATA_DIR)/$$f" ]; then \
			printf 'fetch %s\n' "$$f"; \
			$(CURL) -fsSL --retry 3 -o "$(VP8_TEST_DATA_DIR)/$$f.tmp" "$(LIBVPX_TEST_DATA_BASE)/$$f"; \
			mv "$(VP8_TEST_DATA_DIR)/$$f.tmp" "$(VP8_TEST_DATA_DIR)/$$f"; \
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
