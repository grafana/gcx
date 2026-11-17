//go:build !windows

package config

import "os"

func readConfigFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
