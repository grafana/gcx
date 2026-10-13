//go:build wasip1

package fileutil

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("fileutil: unsupported on wasip1")

func mmap(*os.File, int) ([]byte, error) { return nil, errUnsupported }

func munmap([]byte) error { return errUnsupported }

type unixLock struct{}

func (*unixLock) Release() error { return errUnsupported }

func newLock(string) (Releaser, error) { return nil, errUnsupported }
