package config

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func secureLegacyConfigFile(file *os.File, _ os.FileInfo) bool {
	return checkLegacyFileSecurity(file, false) == nil
}

func privateLegacyBackupFile(file *os.File, _ os.FileInfo) error {
	return checkLegacyFileSecurity(file, true)
}

func currentWindowsUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

func checkLegacyFileSecurity(file *os.File, private bool) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	runtime.KeepAlive(file)
	if err != nil {
		return fmt.Errorf("read file security: %w", err)
	}
	user, err := currentWindowsUserSID()
	if err != nil {
		return fmt.Errorf("get current user SID: %w", err)
	}
	return checkLegacySecurityDescriptor(sd, user, private)
}

// checkLegacySecurityDescriptor accepts only standard allow and deny ACEs.
// Deny ACEs do not cancel unsafe grants in this conservative check.
func checkLegacySecurityDescriptor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID, private bool) error {
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return errors.New("file owner is unavailable")
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	trusted := func(sid *windows.SID) bool {
		return sid.Equals(user) || sid.Equals(system) || sid.Equals(admins)
	}
	if (!private && !owner.Equals(user)) || (private && !trusted(owner)) {
		return errors.New("file owner is not trusted")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return errors.New("file DACL is missing or null")
	}
	unsafeAccess := windows.ACCESS_MASK(windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA |
		windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | windows.DELETE |
		windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_WRITE | windows.GENERIC_ALL)
	if private {
		unsafeAccess |= windows.FILE_READ_DATA | windows.GENERIC_READ
	}
	for i := range dacl.AceCount {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(i), &ace); err != nil {
			return fmt.Errorf("read file ACE: %w", err)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			if ace.Header.AceSize < uint16(unsafe.Sizeof(*ace)) { // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- Sizeof is a constant; it does not dereference ace.
				return errors.New("file ACE is too short")
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- GetAce returns the documented inline SID from an OS descriptor retained by KeepAlive below.
			if !sid.IsValid() {
				return errors.New("file ACE has an invalid SID")
			}
			if ace.Mask&unsafeAccess != 0 && !trusted(sid) {
				return fmt.Errorf("file DACL grants unsafe access to %s", sid.String())
			}
		default:
			return fmt.Errorf("file DACL has an unsupported ACE type: %d", ace.Header.AceType)
		}
	}
	runtime.KeepAlive(sd)
	return nil
}

// createPrivateLegacyBackup applies a protected DACL when it creates the file.
// No secret bytes are written with inherited access from the parent directory.
func createPrivateLegacyBackup(path string) (*os.File, error) {
	user, err := currentWindowsUserSID()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.String() +
		"D:P(A;;FA;;;" + user.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attrs := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})), // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- Constant native struct size for CreateFile; no memory access.
		SecurityDescriptor: sd,
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE|windows.READ_CONTROL,
		0, &attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	runtime.KeepAlive(sd)
	if err != nil {
		return nil, &os.PathError{Op: "create", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err == nil {
		err = privateLegacyBackupFile(file, info)
	}
	if err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return file, nil
}
