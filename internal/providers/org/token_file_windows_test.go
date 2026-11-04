package org //nolint:testpackage // Tests inspect the private file creation helper.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestPrivateTokenFileWindows(t *testing.T) {
	dir := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	// Make the parent readable by Everyone. The token must not inherit that grant.
	parentDescriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")(A;OICI;FR;;;WD)")
	require.NoError(t, err)
	parentDACL, _, err := parentDescriptor.DACL()
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, parentDACL, nil))
	runtime.KeepAlive(parentDescriptor)
	path := filepath.Join(dir, "token.txt")
	file, err := createPrivateTokenFile(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	// Inspect the ACL before the first secret byte is written.
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	require.NoError(t, err)
	control, _, err := descriptor.Control()
	require.NoError(t, err)
	require.NotZero(t, control&windows.SE_DACL_PROTECTED)
	owner, _, err := descriptor.Owner()
	require.NoError(t, err)
	require.True(t, owner.Equals(user.User.Sid))
	dacl, _, err := descriptor.DACL()
	require.NoError(t, err)
	require.NotNil(t, dacl)
	require.EqualValues(t, 3, dacl.AceCount)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	require.NoError(t, err)
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	require.NoError(t, err)
	allowed := map[string]bool{user.User.Sid.String(): false, system.String(): false, admins.String(): false}
	for i := range dacl.AceCount {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, uint32(i), &ace))
		require.EqualValues(t, windows.ACCESS_ALLOWED_ACE_TYPE, ace.Header.AceType)
		require.Zero(t, ace.Header.AceFlags&windows.INHERITED_ACE)
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) // nosemgrep: go.lang.security.audit.unsafe.use-of-unsafe-block -- GetAce supplies an inline SID in the descriptor retained by KeepAlive below.
		require.True(t, sid.IsValid())
		_, trusted := allowed[sid.String()]
		require.True(t, trusted, "unexpected file access for %s", sid.String())
		allowed[sid.String()] = true
		assert.NotZero(t, ace.Mask&windows.FILE_READ_DATA)
		assert.NotZero(t, ace.Mask&windows.FILE_WRITE_DATA)
	}
	runtime.KeepAlive(descriptor)
	for sid, present := range allowed {
		assert.True(t, present, "missing access for %s", sid)
	}
	_, err = file.WriteString("secret\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	duplicate, err := createPrivateTokenFile(path)
	if duplicate != nil {
		_ = duplicate.Close()
	}
	require.ErrorIs(t, err, os.ErrExist)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "secret\n", string(contents))
}
