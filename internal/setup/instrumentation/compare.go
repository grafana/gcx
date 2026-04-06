package instrumentation

// RemoteOnlyNamespace is a namespace present in the remote config that is
// absent from the local manifest.
type RemoteOnlyNamespace struct {
	Namespace string
}

// RemoteOnlyApp is a workload present in the remote config for a namespace
// that exists locally, but the workload itself is not in the local manifest.
type RemoteOnlyApp struct {
	Namespace string
	App       string
}

// Diff holds remote-only items: namespaces and workloads that exist remotely
// but are absent from the local manifest.
//
// An empty Diff means the local manifest is a superset of (or matches) the
// remote config — the apply is safe to proceed.
//
// A non-empty Diff means the apply would silently drop remote state, so the
// caller must fail with a descriptive error listing the remote-only items.
type Diff struct {
	Namespaces []RemoteOnlyNamespace
	Apps       []RemoteOnlyApp
}

// IsEmpty returns true when there are no remote-only items.
func (d *Diff) IsEmpty() bool {
	return len(d.Namespaces) == 0 && len(d.Apps) == 0
}

// Compare computes the set of items present in remote that are NOT in local.
//
//   - A namespace in remote with no matching entry in local → RemoteOnlyNamespace.
//   - An app in a remote namespace that also exists locally, but the app is not
//     in that local namespace → RemoteOnlyApp.
//
// Nil local or remote are treated as empty AppSpec (no namespaces).
func Compare(local, remote *AppSpec) *Diff {
	diff := &Diff{}
	if remote == nil {
		return diff
	}

	// Index local namespaces: name → set of app names.
	localNS := make(map[string]map[string]struct{})
	if local != nil {
		for _, ns := range local.Namespaces {
			apps := make(map[string]struct{}, len(ns.Apps))
			for _, app := range ns.Apps {
				apps[app.Name] = struct{}{}
			}
			localNS[ns.Name] = apps
		}
	}

	for _, remNS := range remote.Namespaces {
		localApps, exists := localNS[remNS.Name]
		if !exists {
			// Entire namespace is remote-only.
			diff.Namespaces = append(diff.Namespaces, RemoteOnlyNamespace{Namespace: remNS.Name})
			continue
		}

		// Namespace present locally — check for remote-only apps within it.
		for _, remApp := range remNS.Apps {
			if _, ok := localApps[remApp.Name]; !ok {
				diff.Apps = append(diff.Apps, RemoteOnlyApp{
					Namespace: remNS.Name,
					App:       remApp.Name,
				})
			}
		}
	}

	return diff
}
