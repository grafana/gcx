//go:build !windows

package main

import "os"

func openTerminal() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }
