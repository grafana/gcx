module github.com/grafana/gcx/experimental/sandbox

go 1.25.0

require github.com/tetratelabs/wazero v1.12.1-0.20261007202231-bd5c0d149588

require golang.org/x/sys v0.44.0 // indirect

// wazero with three fixes not yet upstream: compiled code is mapped from the
// compilation cache file instead of copied into memory, function bodies are
// released after compiling, and a module's memory and files are released
// when the guest exits or traps just after its context is cancelled.
// Together the first two cut a process running gcx from ~730 MiB to
// ~140 MiB of private memory. The require above is the fork commit's
// pseudo-version rather than v1.12.0, so wazero's version, and with it the
// compilation cache key, changes with every fork commit.
//
// replace directives only apply to the main module, so embedders must copy
// both lines: without the replace, Go looks for the required pseudo-version
// in upstream wazero, which doesn't have it, and the build fails. Drop both
// once wazero releases the fixes.
replace github.com/tetratelabs/wazero => github.com/sd2k/wazero v0.0.0-20261007202231-bd5c0d149588
