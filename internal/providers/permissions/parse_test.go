package permissions

import "testing"

// These are white-box tests (package permissions, not permissions_test)
// because parsePermissions is unexported. They cover the data-loss bug where
// parsePermissions silently returned (nil, nil) for JSON shapes that don't
// match either the bare-array or {"permissions":[...]} envelope shape,
// causing Client.Set to POST {"permissions": null} and wipe every managed
// permission on the resource.

func TestParsePermissions_RejectsEmptyArray(t *testing.T) {
	if _, err := parsePermissions([]byte(`[]`)); err == nil {
		t.Fatal("parsePermissions([]) = nil error, want error rejecting empty result")
	}
}

func TestParsePermissions_RejectsNull(t *testing.T) {
	if _, err := parsePermissions([]byte(`null`)); err == nil {
		t.Fatal("parsePermissions(null) = nil error, want error rejecting empty result")
	}
}

func TestParsePermissions_RejectsEmptyEnvelope(t *testing.T) {
	if _, err := parsePermissions([]byte(`{}`)); err == nil {
		t.Fatal(`parsePermissions({}) = nil error, want error rejecting empty result`)
	}
}

func TestParsePermissions_RejectsUnrecognizedEnvelopeShape(t *testing.T) {
	// A natural mistake given the sibling `grant` command's flags: this looks
	// like a permission assignment but isn't the documented envelope shape.
	if _, err := parsePermissions([]byte(`{"userId":1,"permission":"Edit"}`)); err == nil {
		t.Fatal(`parsePermissions({"userId":1,"permission":"Edit"}) = nil error, want error rejecting unrecognized shape`)
	}
}

func TestParsePermissions_RejectsWrongEnvelopeKey(t *testing.T) {
	if _, err := parsePermissions([]byte(`{"perms":[{"permission":"Edit","userId":1}]}`)); err == nil {
		t.Fatal(`parsePermissions({"perms":[...]}) = nil error, want error rejecting unrecognized shape`)
	}
}

func TestParsePermissions_AcceptsBareArray(t *testing.T) {
	perms, err := parsePermissions([]byte(`[{"permission":"Edit","userId":1}]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 1 || perms[0].Permission != "Edit" || perms[0].UserID != 1 {
		t.Fatalf("unexpected parsed permissions: %#v", perms)
	}
}

func TestParsePermissions_AcceptsEnvelope(t *testing.T) {
	perms, err := parsePermissions([]byte(`{"permissions":[{"permission":"Admin","teamId":2}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 1 || perms[0].Permission != "Admin" || perms[0].TeamID != 2 {
		t.Fatalf("unexpected parsed permissions: %#v", perms)
	}
}
