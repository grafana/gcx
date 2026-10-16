//go:build wasip1

package term

import (
	"errors"
	"io"
	"os"
)

// ErrInvalidState is returned if the state of the terminal is invalid.
//
// Deprecated: ErrInvalidState is no longer used.
var ErrInvalidState = errors.New("Invalid terminal state")

var errUnsupported = errors.New("term: unsupported on wasip1")

type terminalState struct{}

func stdStreams() (stdIn io.ReadCloser, stdOut, stdErr io.Writer) {
	return os.Stdin, os.Stdout, os.Stderr
}

func getFdInfo(in interface{}) (uintptr, bool) {
	if file, ok := in.(*os.File); ok {
		return file.Fd(), false
	}
	return 0, false
}

func getWinsize(uintptr) (*Winsize, error)         { return nil, errUnsupported }
func setWinsize(uintptr, *Winsize) error           { return errUnsupported }
func isTerminal(uintptr) bool                      { return false }
func restoreTerminal(uintptr, *State) error        { return errUnsupported }
func saveState(uintptr) (*State, error)            { return nil, errUnsupported }
func disableEcho(uintptr, *State) error            { return errUnsupported }
func setRawTerminal(uintptr) (*State, error)       { return nil, errUnsupported }
func setRawTerminalOutput(uintptr) (*State, error) { return nil, errUnsupported }
func makeRaw(uintptr) (*State, error)              { return nil, errUnsupported }
