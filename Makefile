SHELL := /bin/sh

GO ?= go
GOFMT ?= gofmt
GIT ?= git
CURL ?= curl
AWK ?= awk

GOCACHE ?= $(CURDIR)/.gocache
CORACLE_BUILD := internal/coracle/build
LIBVPX_TEST_DATA_BASE := https://storage.googleapis.com/downloads.webmproject.org/test_data/libvpx
LIBVPX_TEST_DATA_MK := $(CORACLE_BUILD)/libvpx-v1.16.0/test/test-data.mk
ORACLE := $(CORACLE_BUILD)/gopvx-vpx-oracle
VPXENC := $(CORACLE_BUILD)/vpxenc
VPXDEC := $(CORACLE_BUILD)/vpxdec
VP8_TEST_DATA_DIR := $(CORACLE_BUILD)/test-data/vp8
VP8_ENCODER_SOURCE_DIR := $(CORACLE_BUILD)/test-data/encoder

VP8_DECODER_IVF_MIN ?= 58
VP8_INVALID_IVF_MIN ?= 2
VP8_ENCODER_SOURCE_MIN ?= 1
VP8_ENCODER_SOURCE_FRAMES ?= 6
VP8_ENCODER_SOURCE_FILES ?= park_joy_90p_8_420.y4m

.PHONY: all ci fmtcheck test verify verify-production oracle-test oracle-tools fetch-test-data fetch-vp8-test-data fetch-encoder-test-data

all: ci

ci: fmtcheck test

fmtcheck:
	files="$$($(GOFMT) -l $$($(GIT) ls-files '*.go'))"; \
	if [ -n "$$files" ]; then \
		printf 'gofmt needed:\n%s\n' "$$files"; \
		exit 1; \
	fi

verify: ci

verify-production: test oracle-test

test:
	GOCACHE="$(GOCACHE)" $(GO) test ./... -count=1

oracle-test: oracle-tools fetch-test-data
	GOCACHE="$(GOCACHE)" \
	GOPVX_WITH_ORACLE=1 \
	GOPVX_ORACLE="$(ORACLE)" \
	GOPVX_VPXDEC="$(VPXDEC)" \
	GOPVX_VPXENC="$(VPXENC)" \
	GOPVX_TEST_DATA_PATH="$(VP8_TEST_DATA_DIR)" \
	GOPVX_TEST_DATA_REQUIRED=1 \
	GOPVX_TEST_DATA_MIN="$(VP8_DECODER_IVF_MIN)" \
	GOPVX_INVALID_TEST_DATA_REQUIRED=1 \
	GOPVX_INVALID_TEST_DATA_MIN="$(VP8_INVALID_IVF_MIN)" \
	GOPVX_ENCODER_TEST_DATA_PATH="$(VP8_ENCODER_SOURCE_DIR)" \
	GOPVX_ENCODER_TEST_DATA_REQUIRED=1 \
	GOPVX_ENCODER_TEST_DATA_MIN="$(VP8_ENCODER_SOURCE_MIN)" \
	GOPVX_ENCODER_TEST_DATA_FRAMES="$(VP8_ENCODER_SOURCE_FRAMES)" \
	$(GO) test . -run 'TestOracle' -count=1 -timeout 10m

oracle-tools: $(ORACLE)
	internal/coracle/build_vpxenc.sh >/dev/null
	test -x "$(VPXENC)"
	test -x "$(VPXDEC)"

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
