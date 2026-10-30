//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestLegacySecurityDescriptorWindows(t *testing.T) {
	user, err := currentWindowsUserSID()
	if err != nil {
		t.Fatal(err)
	}
	owner := "O:" + user.String()
	userACE := "(A;;FA;;;" + user.String() + ")"
	tests := []struct {
		name    string
		sddl    string
		legacy  bool
		private bool
	}{
		{name: "private", sddl: owner + "D:P" + userACE, legacy: true, private: true},
		{name: "trusted principals", sddl: owner + "D:P" + userACE + "(A;;FA;;;SY)(A;;FA;;;BA)", legacy: true, private: true},
		{name: "public read", sddl: owner + "D:P" + userACE + "(A;;FR;;;WD)", legacy: true},
		{name: "public write", sddl: owner + "D:P" + userACE + "(A;;FW;;;WD)"},
		{name: "public append", sddl: owner + "D:P" + userACE + "(A;;0x4;;;WD)"},
		{name: "public write EA", sddl: owner + "D:P" + userACE + "(A;;0x10;;;WD)"},
		{name: "public write attributes", sddl: owner + "D:P" + userACE + "(A;;0x100;;;WD)"},
		{name: "public delete", sddl: owner + "D:P" + userACE + "(A;;SD;;;WD)"},
		{name: "public change DACL", sddl: owner + "D:P" + userACE + "(A;;WD;;;WD)"},
		{name: "public change owner", sddl: owner + "D:P" + userACE + "(A;;WO;;;WD)"},
		{name: "public generic write", sddl: owner + "D:P" + userACE + "(A;;GW;;;WD)"},
		{name: "public generic all", sddl: owner + "D:P" + userACE + "(A;;GA;;;WD)"},
		{name: "public generic read", sddl: owner + "D:P" + userACE + "(A;;GR;;;WD)", legacy: true},
		{name: "public metadata", sddl: owner + "D:P" + userACE + "(A;;RC;;;WD)", legacy: true, private: true},
		{name: "inherit only", sddl: owner + "D:P" + userACE + "(A;OIIO;FA;;;WD)", legacy: true, private: true},
		{name: "deny alone", sddl: owner + "D:P(D;;FW;;;WD)" + userACE, legacy: true, private: true},
		{name: "deny does not cancel grant", sddl: owner + "D:P(D;;FW;;;WD)" + userACE + "(A;;FW;;;WD)"},
		{name: "unsupported object ACE", sddl: owner + "D:P" + userACE + "(OA;;FW;00112233-4455-6677-8899-aabbccddeeff;;WD)"},
		{name: "untrusted owner", sddl: "O:WDD:P" + userACE},
		{name: "administrator owner", sddl: "O:BAD:P" + userACE, private: true},
		{name: "missing DACL", sddl: owner},
		{name: "null DACL", sddl: owner + "D:NO_ACCESS_CONTROL"},
		{name: "empty DACL", sddl: owner + "D:P", legacy: true, private: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tt.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkLegacySecurityDescriptor(sd, user, false); (err == nil) != tt.legacy {
				t.Errorf("legacy security error = %v, want accepted = %v", err, tt.legacy)
			}
			if err := checkLegacySecurityDescriptor(sd, user, true); (err == nil) != tt.private {
				t.Errorf("backup security error = %v, want accepted = %v", err, tt.private)
			}
		})
	}
}

func TestSecureLegacyConfigIdentityWindows(t *testing.T) {
	for _, tt := range []struct {
		name    string
		grant   string
		trusted bool
	}{
		{name: "private", trusted: true},
		{name: "public read", grant: "(A;;FR;;;WD)", trusted: true},
		{name: "public write", grant: "(A;;FW;;;WD)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			file := createWindowsSecurityTestFile(t, path, tt.grant)
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			_, trusted := secureLegacyConfigIdentity(path)
			if trusted != tt.trusted {
				t.Errorf("trusted = %v, want %v", trusted, tt.trusted)
			}
		})
	}
}

func TestCreatePrivateLegacyBackupWindows(t *testing.T) {
	dir := t.TempDir()
	user, err := currentWindowsUserSID()
	if err != nil {
		t.Fatal(err)
	}
	// Make the parent public. The backup must not inherit this access.
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.String() + ")(A;OICI;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.legacy.bak")
	file, err := createPrivateLegacyBackup(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// Inspect the ACL before writing any plaintext.
	if err := privateLegacyBackupFile(file, info); err != nil {
		t.Fatal(err)
	}
	createdSD, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := createdSD.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("backup DACL is not protected from inheritance")
	}
	if _, err := file.WriteString("secret"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := readPrivateLegacyBackup(path)
	if err != nil || string(contents) != "secret" {
		t.Fatalf("backup contents = %q, error = %v", contents, err)
	}
	if duplicate, err := createPrivateLegacyBackup(path); !errors.Is(err, os.ErrExist) {
		if duplicate != nil {
			_ = duplicate.Close()
		}
		t.Fatalf("second create error = %v, want os.ErrExist", err)
	}
}

func TestReadPrivateLegacyBackupWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.legacy.bak")
	file := createWindowsSecurityTestFile(t, path, "(A;;FR;;;WD)")
	if _, err := file.WriteString("secret"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateLegacyBackup(path); err == nil {
		t.Fatal("public backup was accepted")
	}
}

func createWindowsSecurityTestFile(t *testing.T, path, extraACE string) *os.File {
	t.Helper()
	user, err := currentWindowsUserSID()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + user.String() +
		"D:P(A;;FA;;;" + user.String() + ")" + extraACE)
	if err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attrs := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ, &attrs, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(handle), path)
}

func makeLegacyConfigWritableByOthers(t *testing.T, path string) {
	t.Helper()
	user, err := currentWindowsUserSID()
	if err != nil {
		t.Fatal(err)
	}
	setMigrationTestDACL(t, path, "D:P(A;;FA;;;"+user.String()+")(A;;FW;;;WD)")
}

func makeMigrationDirectoryReadOnly(t *testing.T, path string) {
	t.Helper()
	user, err := currentWindowsUserSID()
	if err != nil {
		t.Fatal(err)
	}
	// Restore full access before TempDir cleanup.
	t.Cleanup(func() {
		setMigrationTestDACL(t, path, "D:P(A;OICI;FA;;;"+user.String()+")")
	})
	setMigrationTestDACL(t, path, "D:P(A;;FRFX;;;"+user.String()+")")
}

func setMigrationTestDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}
