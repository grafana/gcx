module github.com/grafana/gcx/experimental/sandbox

go 1.25.0

require github.com/tetratelabs/wazero v1.12.1-0.20261007163245-120e6b8e4515

require golang.org/x/sys v0.44.0 // indirect

// wazero with two fixes not yet upstream: compiled code is mapped from the
// compilation cache file instead of copied into memory, and function bodies
// are released after compiling. Together they cut a process running gcx from
// ~730 MiB to ~140 MiB of private memory. The require above is the fork
// commit's pseudo-version rather than v1.12.0, so wazero's version, and with
// it the compilation cache key, changes with every fork commit.
//
// replace directives only apply to the main module: embedders must copy both
// lines, or they run upstream wazero and miss the precompiled cache in
// ghcr.io/grafana/gcx-wasm. Drop both once wazero releases the fixes.
replace github.com/tetratelabs/wazero => github.com/sd2k/wazero v0.0.0-20261007163245-120e6b8e4515
