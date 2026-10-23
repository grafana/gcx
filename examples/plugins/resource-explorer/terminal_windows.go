package main

import (
	"errors"
	"os"
)

func openTerminal() (*os.File, error) {
	return nil, errors.New("interactive Windows terminals are not implemented in this PoC; use --no-tui")
}
