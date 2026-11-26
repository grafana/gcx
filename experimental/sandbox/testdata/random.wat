;; A guest for the sandbox's entropy test: it fills 16 bytes with WASI
;; random_get and writes them to stdout, or traps if random_get fails. Two
;; runs must print different bytes.
;;
;; Rebuild with: wasm-tools parse testdata/random.wat -o testdata/random.wasm
(module
  (import "wasi_snapshot_preview1" "random_get"
    (func $random_get (param i32 i32) (result i32)))
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))
  (memory (export "memory") 1)
  (func (export "_start")
    (if (call $random_get (i32.const 16) (i32.const 16))
      (then unreachable))
    ;; One iovec at 0: the 16 bytes at 16. The count written goes to 8.
    (i32.store (i32.const 0) (i32.const 16))
    (i32.store (i32.const 4) (i32.const 16))
    (drop (call $fd_write (i32.const 1) (i32.const 0) (i32.const 1) (i32.const 8)))))
