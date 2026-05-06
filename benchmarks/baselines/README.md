# Decode Baselines

Capture JSON benchmark output locally:

```sh
go test ./benchmarks -run '^$' -bench 'BenchmarkDecode' -benchmem -json > benchmarks/baselines/decode.local.json
```

Include the opt-in libvpx reference benchmark by setting:

```sh
LIBGOPX_WITH_ORACLE=1 LIBGOPX_ORACLE=internal/coracle/build/gopx-vpx-oracle go test ./benchmarks -run '^$' -bench 'BenchmarkDecode' -benchmem -json
```

The checked-in decode benchmarks use the libvpx v1.16.0 smoke IVF stream from
`internal/testutil`.
