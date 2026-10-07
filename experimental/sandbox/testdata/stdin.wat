;; A guest for the sandbox's lifecycle tests, standing in for gcx. It reads
;; up to 16 bytes from stdin, then, by the first byte:
;;
;;   "t"        traps
;;   "s"        spins forever, until it's stopped
;;   otherwise  writes what it read to stdout and exits with status 0
;;
;; Reading stdin is where tests hold it, so they can cancel or close the
;; runtime while it's inside a host call; checking the byte afterwards touches
;; its memory.
;;
;; Rebuild with: wasm-tools parse testdata/stdin.wat -o testdata/stdin.wasm
(module
  (import "wasi_snapshot_preview1" "fd_read"
    (func $fd_read (param i32 i32 i32 i32) (result i32)))
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))
  (import "wasi_snapshot_preview1" "proc_exit" (func $proc_exit (param i32)))
  (memory (export "memory") 1)
  (func (export "_start")
    ;; One iovec at 0: 16 bytes at 16. The count read goes to 8.
    (i32.store (i32.const 0) (i32.const 16))
    (i32.store (i32.const 4) (i32.const 16))
    (drop (call $fd_read (i32.const 0) (i32.const 0) (i32.const 1) (i32.const 8)))
    (if (i32.eq (i32.load8_u (i32.const 16)) (i32.const 116))
      (then unreachable))
    (if (i32.eq (i32.load8_u (i32.const 16)) (i32.const 115))
      (then (loop $spin (br $spin))))
    ;; Write back what was read: the iovec's length becomes the count read.
    (i32.store (i32.const 4) (i32.load (i32.const 8)))
    (drop (call $fd_write (i32.const 1) (i32.const 0) (i32.const 1) (i32.const 8)))
    (call $proc_exit (i32.const 0))))
