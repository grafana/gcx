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
		case "flood":
			// Output written just before returning must still reach gcx.
			fmt.Print(strings.Repeat("x", 2<<20) + "END\n")
			return errors.New("after output")
		case "wait":
			<-ctx.Done()
			return ctx.Err()
		}
		return fmt.Errorf("unknown test command %q", args[0])
	})
}
