#!/bin/sh
# Verify that both supported SIMD architectures select native kernels, and
# that purego and non-experimental builds retain an independent scalar path.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

for arch in amd64 arm64; do
	for mode in native purego scalar; do
		tags=
		experiments=simd
		case "$mode" in
			purego) tags=purego ;;
			scalar) experiments= ;;
		esac
		# Inspect repository imports: the experimental standard library may
		# itself use archsimd even when our kernels select their scalar path.
		imports=$(GOARCH="$arch" GOEXPERIMENT="$experiments" scripts/go.sh list -tags "$tags" -f '{{range .Imports}}{{println .}}{{end}}' ./...)
		if printf '%s\n' "$imports" | grep -qx 'simd/archsimd'; then
			if [ "$mode" != native ]; then
				printf '%s/%s unexpectedly imports simd/archsimd\n' "$arch" "$mode" >&2
				exit 1
			fi
		elif [ "$mode" = native ]; then
			printf '%s/native does not import simd/archsimd\n' "$arch" >&2
			exit 1
		fi
		printf '%s/%s build selection OK\n' "$arch" "$mode"
	done
done
