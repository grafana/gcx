//go:build !windows

package config

import (
	"fmt"
	"os"
	"os/user"
	"reflect"
	"strconv"
)

func secureLegacyConfigFile(_ *os.File, info os.FileInfo) bool {
	return info.Mode().Perm()&0o022 == 0 && legacyConfigOwnedByCurrentUser(info)
}

func privateLegacyBackupFile(_ *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("mode %s", info.Mode().Perm())
	}
	return nil
}

func createPrivateLegacyBackup(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, configFilePermissions)
}

// legacyConfigOwnedByCurrentUser checks numeric ownership where it is available.
func legacyConfigOwnedByCurrentUser(info os.FileInfo) bool {
	stat := reflect.ValueOf(info.Sys())
	if !stat.IsValid() {
		return true
	}
	if stat.Kind() == reflect.Pointer {
		if stat.IsNil() {
			return true
		}
		stat = stat.Elem()
	}
	if stat.Kind() != reflect.Struct {
		return true
	}
	uid := stat.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return true
	}
	current, err := user.Current()
	if err != nil {
		return false
	}
	return current.Uid == strconv.FormatUint(uid.Uint(), 10)
}
