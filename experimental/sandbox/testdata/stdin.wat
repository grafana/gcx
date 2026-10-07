;; A guest for the sandbox's lifecycle tests, standing in for gcx: it reads
;; up to 16 bytes from stdin, traps if the first byte is "t", and otherwise
;; exits with status 0. Reading stdin is where tests hold it, so they can
;; cancel or close the runtime while it's inside a host call, and the read
;; of the byte afterwards touches its memory.
;;
;; Rebuild with: wasm-tools parse testdata/stdin.wat -o testdata/stdin.wasm
(module
  (import "wasi_snapshot_preview1" "fd_read"
    (func $fd_read (param i32 i32 i32 i32) (result i32)))
  (import "wasi_snapshot_preview1" "proc_exit" (func $proc_exit (param i32)))
  (memory (export "memory") 1)
  (func (export "_start")
    ;; One iovec at 0: 16 bytes at 16. The count read goes to 8.
    (i32.store (i32.const 0) (i32.const 16))
    (i32.store (i32.const 4) (i32.const 16))
    (drop (call $fd_read (i32.const 0) (i32.const 0) (i32.const 1) (i32.const 8)))
    (if (i32.eq (i32.load8_u (i32.const 16)) (i32.const 116))
      (then unreachable))
    (call $proc_exit (i32.const 0))))
