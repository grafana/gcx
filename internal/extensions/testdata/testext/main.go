// Command testext is an extension used by the extensions package tests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/extsdk"
)

func main() {
	extsdk.Serve(func(ctx context.Context, gcx *extsdk.GCX, args []string) error {
		switch args[0] {
		case "echo":
			fmt.Println(args[1:])
			fmt.Fprintln(os.Stderr, "to stderr")
			return nil
		case "exit":
			code, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}
			return &extsdk.ExitError{Code: code}
		case "gcx":
			var out any
			if err := gcx.DecodeJSON(ctx, &out, args[1:]...); err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		case "size":
			res, err := gcx.Execute(ctx, extsdk.Request{Args: args[1:]})
			if err != nil {
				return err
			}
			fmt.Println(len(res.Stdout), res.ExitCode)
			return nil
		case "flood":
			// Output written just before returning must still reach gcx.
			fmt.Print(strings.Repeat("x", 2<<20) + "END\n")
			return errors.New("after output")
		case "stream":
			// Stopping the command at its first output only works if
			// Request.Stdout gets output before the command exits.
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			_, err := gcx.Execute(ctx, extsdk.Request{Args: args[1:], Stdout: writerFunc(func(p []byte) (int, error) {
				cancel()
				return len(p), nil
			})})
			if ctx.Err() == nil {
				return fmt.Errorf("command exited before its output arrived: %v", err)
			}
			fmt.Println("streamed")
			return nil
		case "wait":
			<-ctx.Done()
			return ctx.Err()
		}
		return fmt.Errorf("unknown test command %q", args[0])
	})
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
