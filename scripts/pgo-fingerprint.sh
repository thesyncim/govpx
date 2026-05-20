#!/bin/sh
set -eu

git_cmd=${GIT:-git}

"$git_cmd" ls-files --cached --others --exclude-standard '*.go' '*.s' go.mod go.sum Makefile scripts/pgo-fingerprint.sh |
	awk '
		/_test\.go$/ { next }
		$0 == "Makefile" { print; next }
		$0 == "go.mod" { print; next }
		$0 == "go.sum" { print; next }
		$0 == "scripts/pgo-fingerprint.sh" { print; next }
		/^cmd\/govpx-bench\// { print; next }
		/^internal\/cpu\// { print; next }
		/^internal\/vp8\// { print; next }
		/^vp8_encoder/ { print; next }
		/^vp8_decoder/ { print; next }
		$0 == "options.go" { print; next }
		$0 == "timing.go" { print; next }
		/^vp8_ratecontrol/ { print; next }
		$0 == "temporal.go" { print; next }
		$0 == "codec.go" { print; next }
		$0 == "errors.go" { print; next }
		$0 == "image.go" { print; next }
	' |
	LC_ALL=C sort |
	while IFS= read -r file; do
		[ -n "$file" ] || continue
		if [ "${file##*.}" = "go" ] && head -n 5 "$file" | grep -qx '//go:build govpx_oracle_trace'; then
			continue
		fi
		printf '%s  %s\n' "$("$git_cmd" hash-object "$file")" "$file"
	done |
	"$git_cmd" hash-object --stdin
