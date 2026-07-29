package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/grafana-app-sdk/logging"
)

// lazyStore defers opening the keychain backend until the first Get/Set/Delete.
// resolveSentinelsForOwner only calls Get for fields that actually hold a
// sentinel, and transactional config writes only call Set when plaintext
// secrets are present, so a config whose current context has no keychain-backed
// secrets never probes the OS keychain at all. Once opened, the backend is reused.
type lazyStore struct {
	once    sync.Once
	open    func() credentials.Store
	backend credentials.Store
}

func newLazyStore(open func() credentials.Store) *lazyStore {
	return &lazyStore{open: open}
}

func (l *lazyStore) resolve() credentials.Store {
	l.once.Do(func() { l.backend = l.open() })
	return l.backend
}

func (l *lazyStore) Get(key string) (string, error) { return l.resolve().Get(key) }
func (l *lazyStore) Set(key, value string) error    { return l.resolve().Set(key, value) }
func (l *lazyStore) Delete(key string) error        { return l.resolve().Delete(key) }

// keychainBacked tracks which (owner, field) pairs were stored in the keychain
// at load time. Owners are "stack:<name>" / "cloud:<name>" keys. The map lives
// on Config as an unexported field; it is populated during Load and consumed by
// reconcileKeychain during Write to round-trip sentinel values to disk.
type keychainBacked map[string]map[credentials.Field]bool

func (k keychainBacked) mark(owner string, field credentials.Field) {
	if k[owner] == nil {
		k[owner] = make(map[credentials.Field]bool)
	}
	k[owner][field] = true
}

// keychainPreserved tracks (owner, field) pairs whose sentinel could not be
// resolved at load time (keychain unavailable), mapped to the original
// sentinel string. Write round-trips the original verbatim — it may be a
// legacy-format sentinel whose value lives under a legacy account key, so it
// must not be re-derived from the owner.
type keychainPreserved map[string]map[credentials.Field]string

func (k keychainPreserved) mark(owner string, field credentials.Field, sentinel string) {
	if k[owner] == nil {
		k[owner] = make(map[credentials.Field]string)
	}
	k[owner][field] = sentinel
}

type keychainStateKey struct {
	source string
	owner  string
	field  credentials.Field
}

type keychainStateStatus uint8

const (
	keychainStateBacked keychainStateStatus = iota + 1
	keychainStateUnresolved
	keychainStatePreserved
	keychainStateRejected
	keychainStateMissing
)

type keychainFieldState struct {
	binding   credentials.Binding
	account   string
	sentinel  string
	plaintext string
	status    keychainStateStatus
}

type keychainState map[keychainStateKey]keychainFieldState

type secretMutationSet map[keychainStateKey]bool

type credentialOrigin struct {
	binding credentials.Binding
	value   string
}

type credentialOriginSet map[keychainStateKey]credentialOrigin

// secretRef is a get/set handle for a secret field. Provider-map secrets
// cannot be addressed by *string (Go map values are not addressable), so all
// callers go through this interface uniformly.
type secretRef struct {
	get func() string
	set func(string)
}

// secretOwner identifies one keychain owner — a stack entry or a cloud auth
// entry — together with the secret fields it may hold.
type secretOwner struct {
	key         string // canonical owner, e.g. "stack:prod" or "cloud:grafana-com"
	source      string
	layer       string
	fields      []credentials.Field
	ref         func(field credentials.Field) (secretRef, bool)
	destination func(field credentials.Field) string
	reject      func(field credentials.Field, reason string)
	clearReject func(field credentials.Field)
}

//nolint:gochecknoglobals // constant-like lookup lists; never mutated.
var (
	stackSecretFields = []credentials.Field{
		credentials.FieldGrafanaToken,
		credentials.FieldGrafanaPassword,
		credentials.FieldOAuthToken,
		credentials.FieldOAuthRefreshToken,
		credentials.FieldSMToken,
	}
	cloudSecretFields = []credentials.Field{
		credentials.FieldCloudToken,
		credentials.FieldOAuthToken,
	}
)

func stackOwner(name string, stack *StackConfig) secretOwner {
	return secretOwner{
		key:         credentials.StackOwner(name),
		source:      stack.sourceIdentity,
		layer:       stack.sourceLayer,
		fields:      stackSecretFields,
		ref:         func(field credentials.Field) (secretRef, bool) { return stackFieldRef(stack, field) },
		destination: func(field credentials.Field) string { return stackSecretDestination(stack, field) },
		reject:      stack.rejectCredential,
		clearReject: stack.clearCredentialRejection,
	}
}

func cloudOwner(name string, entry *CloudEntry) secretOwner {
	return secretOwner{
		key:         credentials.CloudOwner(name),
		source:      entry.sourceIdentity,
		layer:       entry.sourceLayer,
		fields:      cloudSecretFields,
		ref:         func(field credentials.Field) (secretRef, bool) { return cloudFieldRef(entry, field) },
		destination: func(credentials.Field) string { return cloudSecretDestination(entry) },
		reject:      entry.rejectCredential,
		clearReject: entry.clearCredentialRejection,
	}
}

func (owner secretOwner) stateKey(field credentials.Field) keychainStateKey {
	return keychainStateKey{source: owner.source, owner: owner.key, field: field}
}

func (owner secretOwner) binding(field credentials.Field) credentials.Binding {
	return credentials.Binding{
		Source:      owner.source,
		Owner:       owner.key,
		Field:       field,
		Destination: owner.destination(field),
	}
}

// secretOwners enumerates every keychain owner in the config, referenced by a
// context or not, so reconcile and plaintext migration cover orphaned entries.
func (cfg *Config) secretOwners() []secretOwner {
	owners := make([]secretOwner, 0, len(cfg.Stacks)+len(cfg.Cloud))
	for name, stack := range cfg.Stacks {
		if stack != nil {
			owners = append(owners, stackOwner(name, stack))
		}
	}
	for name, entry := range cfg.Cloud {
		if entry != nil {
			owners = append(owners, cloudOwner(name, entry))
		}
	}
	return owners
}

func canonicalConfigSource(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	}
	// A new config file may not exist yet. Canonicalize its parent so aliases
	// through a symlinked directory still share one credential namespace.
	parent := filepath.Dir(abs)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(abs)), nil
	}
	return abs, nil
}

func canonicalConfigSourceForLayer(path, layer string) (string, error) {
	if layer != "local" {
		return canonicalConfigSource(path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing auto-discovered local config %s: file must be regular (symlinks are not allowed)", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	parent := filepath.Dir(filepath.Clean(abs))
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		parent = resolvedParent
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

func (cfg *Config) bindSourceIdentity(source string) {
	cfg.sourceIdentity = source
	for _, stack := range cfg.Stacks {
		if stack != nil && stack.sourceIdentity == "" {
			stack.sourceIdentity = source
			stack.sourceLayer = cfg.sourceLayer
		}
	}
	for _, entry := range cfg.Cloud {
		if entry != nil && entry.sourceIdentity == "" {
			entry.sourceIdentity = source
			entry.sourceLayer = cfg.sourceLayer
		}
	}
}

// capturePlaintextCredentialOrigins snapshots loaded plaintext before any
// override can redirect its destination. This covers keychain-unavailable and
// intentionally plaintext configs; successfully resolved sentinels are tracked
// by keychainStates instead.
func (cfg *Config) capturePlaintextCredentialOrigins() {
	if cfg.credentialOrigins == nil {
		cfg.credentialOrigins = credentialOriginSet{}
	}
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			ref, ok := owner.ref(field)
			if !ok {
				continue
			}
			value := ref.get()
			if value == "" || credentials.IsSentinel(value) {
				continue
			}
			cfg.credentialOrigins[owner.stateKey(field)] = credentialOrigin{
				binding: owner.binding(field),
				value:   value,
			}
		}
	}
}

func stackSecretDestination(stack *StackConfig, field credentials.Field) string {
	grafanaDestination := "server=" + grafanaServerDestination(stack) +
		"|proxy=" + grafanaProxyDestination(stack) +
		"|tls=" + grafanaTLSDestination(stack)
	if field == credentials.FieldSMToken {
		var smURL string
		if stack.Providers != nil && stack.Providers["synth"] != nil {
			smURL = stack.Providers["synth"]["sm-url"]
		}
		return "synth|endpoint=" + normalizeCredentialURL(smURL, "") +
			"|grafana=" + grafanaDestination
	}
	if field == credentials.FieldGrafanaPassword && stack != nil && stack.Grafana != nil {
		grafanaDestination += "|user=" + stack.Grafana.User
	}
	return "grafana|" + grafanaDestination
}

// GrafanaBearerCredentialDestinationMatches reports whether two Grafana
// configurations authorize bearer credentials for the same normalized
// server, proxy, and TLS destination. OAuth and service-account tokens share
// this destination shape; Basic auth additionally binds the username and must
// not use this helper.
//
// Evaluating the destination captures any file-backed TLS material exactly as
// the keychain binding does. Callers can therefore use this before network or
// persistence without reimplementing the security-critical fingerprint rules.
func GrafanaBearerCredentialDestinationMatches(left, right *GrafanaConfig) bool {
	if left == nil || right == nil {
		return false
	}
	leftStack := &StackConfig{Grafana: left}
	rightStack := &StackConfig{Grafana: right}
	return stackSecretDestination(leftStack, credentials.FieldGrafanaToken) ==
		stackSecretDestination(rightStack, credentials.FieldGrafanaToken)
}

// GrafanaTokenBindingMatches reports whether a stored service-account token's
// complete source/owner/field/destination binding matches the effective login
// destination. requestedServer is the CLI-selected server after flag and
// environment precedence; the effective context supplies proxy and TLS state.
// A missing or unbound context fails closed.
func GrafanaTokenBindingMatches(stored, effective *Context, requestedServer string) bool {
	storedBinding, ok := grafanaTokenBinding(stored, "")
	if !ok {
		return false
	}
	effectiveBinding, ok := grafanaTokenBinding(effective, requestedServer)
	return ok && storedBinding == effectiveBinding
}

func grafanaTokenBinding(context *Context, serverOverride string) (credentials.Binding, bool) {
	if context == nil || context.StackEntry == nil || context.Grafana == nil {
		return credentials.Binding{}, false
	}
	stack := *context.StackEntry
	grafana := *context.Grafana
	if serverOverride != "" {
		grafana.Server = serverOverride
	}
	stack.Grafana = &grafana
	binding := stackOwner(context.stackName(), &stack).binding(credentials.FieldGrafanaToken)
	return binding, binding.Valid()
}

func grafanaServerDestination(stack *StackConfig) string {
	if stack == nil || stack.Grafana == nil {
		return ""
	}
	return normalizeCredentialURL(stack.Grafana.Server, "")
}

func grafanaProxyDestination(stack *StackConfig) string {
	if stack == nil || stack.Grafana == nil {
		return ""
	}
	return normalizeCredentialURL(stack.Grafana.ProxyEndpoint, "")
}

func grafanaTLSDestination(stack *StackConfig) string {
	tlsConfig := (*TLS)(nil)
	if stack != nil && stack.Grafana != nil {
		tlsConfig = stack.Grafana.TLS
	}
	hash := sha256.New()
	writeComponent := func(value string) {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	if tlsConfig != nil {
		tlsConfig.captureCredentialFileSnapshots()
		insecure := ""
		if tlsConfig.Insecure {
			insecure = "insecure"
		}
		writeComponent(insecure)
		writeComponent(strings.ToLower(strings.TrimSpace(tlsConfig.ServerName)))
		writeComponent(tlsConfig.effectiveTLSMaterialFingerprint(tlsConfig.CertFile, tlsConfig.CertData, tlsConfig.credentialCertFile))
		writeComponent(tlsConfig.effectiveTLSMaterialFingerprint(tlsConfig.KeyFile, tlsConfig.KeyData, tlsConfig.credentialKeyFile))
		writeComponent(tlsConfig.effectiveTLSMaterialFingerprint(tlsConfig.CAFile, tlsConfig.CAData, tlsConfig.credentialCAFile))
		protocolCount := ""
		if len(tlsConfig.NextProtos) > 0 {
			protocolCount = strconv.Itoa(len(tlsConfig.NextProtos))
		}
		writeComponent(protocolCount)
		for _, protocol := range tlsConfig.NextProtos {
			writeComponent(protocol)
		}
	} else {
		for range 6 {
			writeComponent("")
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (cfg *TLS) captureCredentialFileSnapshots() {
	if cfg == nil {
		return
	}
	// Environment overrides are applied after the file-backed config has been
	// inventoried. If an override changes a TLS path, the original snapshot is
	// no longer the effective connection identity: recapture the new paths once
	// and make both credential binding and transport construction use those exact
	// bytes. An unchanged path deliberately keeps its first snapshot so a later
	// file swap cannot cross the validate-to-use boundary.
	if cfg.credentialFilesCaptured &&
		cfg.credentialCertFile.path == cfg.CertFile &&
		cfg.credentialKeyFile.path == cfg.KeyFile &&
		cfg.credentialCAFile.path == cfg.CAFile {
		return
	}
	cfg.credentialFilesCaptured = true
	cfg.credentialCertFile = snapshotTLSFile(cfg.CertFile)
	cfg.credentialKeyFile = snapshotTLSFile(cfg.KeyFile)
	cfg.credentialCAFile = snapshotTLSFile(cfg.CAFile)
}

func snapshotTLSFile(path string) tlsFileSnapshot {
	snapshot := tlsFileSnapshot{path: path}
	if path == "" {
		return snapshot
	}
	identity, err := filepath.Abs(path)
	if err != nil {
		identity = path
	}
	snapshot.contents, snapshot.err = os.ReadFile(path)
	if snapshot.err != nil {
		status := "unreadable"
		if os.IsNotExist(snapshot.err) {
			status = "missing"
		}
		snapshot.fingerprint = identity + "|" + status
		return snapshot
	}
	digest := sha256.Sum256(snapshot.contents)
	snapshot.fingerprint = fmt.Sprintf("%s|sha256:%x", identity, digest)
	return snapshot
}

func fingerprintTLSFile(path string) string {
	return snapshotTLSFile(path).fingerprint
}

func (cfg *TLS) effectiveTLSMaterialFingerprint(path string, data []byte, captured tlsFileSnapshot) string {
	if path != "" {
		if cfg.credentialFilesCaptured && captured.path == path {
			return captured.fingerprint
		}
		return fingerprintTLSFile(path)
	}
	if len(data) == 0 {
		return ""
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("data-sha256:%x", digest)
}

func cloudSecretDestination(entry *CloudEntry) string {
	return "cloud|api=" + normalizeCredentialURL(entry.APIUrl, "https://grafana.com") +
		"|oauth=" + normalizeCredentialURL(entry.OAuthUrl, "https://grafana.com")
}

// materializeCloudCredentialDestinations makes every credential-bearing Cloud
// entry self-contained. Runtime endpoint resolution must never depend on a
// separately selected stack, because a higher-layer context could otherwise
// redirect the entry's credential to another Cloud environment.
//
//nolint:nestif // Endpoint materialization intentionally keeps every derivation under the credential-bearing entry gate.
func (cfg *Config) materializeCloudCredentialDestinations() error {
	for name, entry := range cfg.Cloud {
		if entry == nil || (entry.Token == "" && entry.OAuthToken == "") {
			continue
		}
		// Either explicit endpoint is an environment anchor. Never derive the
		// missing API endpoint from a context when OAuth already names an
		// environment (or vice versa), as that would make one credential usable
		// against two different Cloud roots.
		if entry.APIUrl == "" && entry.OAuthUrl != "" {
			entry.APIUrl = entry.OAuthUrl
		}
		if entry.OAuthUrl == "" && entry.APIUrl != "" {
			entry.OAuthUrl = entry.APIUrl
		}
		if entry.APIUrl == "" {
			roots := map[string]bool{}
			for _, ctx := range cfg.Contexts {
				if ctx == nil || ctx.Cloud != name {
					continue
				}
				root := "https://grafana.com"
				if ctx.Grafana != nil {
					if derived, ok := GCOMRootFromServerURL(ctx.Grafana.Server); ok {
						root = derived
					}
				}
				roots[normalizeCredentialURL(root, "https://grafana.com")] = true
			}
			if len(roots) > 1 {
				return ambiguousCloudCredentialDestinationError(cfg, name, entry, roots)
			}
			entry.APIUrl = "https://grafana.com"
			for root := range roots {
				entry.APIUrl = root
			}
		}
		if entry.OAuthUrl == "" {
			entry.OAuthUrl = entry.APIUrl
		}
	}
	return nil
}

func ambiguousCloudCredentialDestinationError(
	cfg *Config,
	name string,
	entry *CloudEntry,
	roots map[string]bool,
) error {
	environments := make([]string, 0, len(roots))
	for root := range roots {
		environments = append(environments, root)
	}
	slices.Sort(environments)

	source := cfg.Source
	if source == "" {
		source = entry.sourceIdentity
	}
	repairCommand := "gcx config edit"
	switch entry.sourceLayer {
	case "system", "user", "local":
		repairCommand += " " + entry.sourceLayer
	default:
		if source != "" {
			repairCommand += " --config " + strconv.Quote(source)
		}
	}

	return gcxerrors.DetailedError{
		Summary: "Cloud credential destination is ambiguous",
		Details: fmt.Sprintf(
			"Cloud entry %q in %s is referenced by contexts in different Cloud environments: %s. gcx will not guess which destination may receive this credential.",
			name,
			source,
			strings.Join(environments, ", "),
		),
		Suggestions: []string{
			"Open the owning file without loading it: " + repairCommand,
			fmt.Sprintf("Split cloud.%s into one entry per environment and update each context.cloud binding", name),
		},
	}
}

// inventoryBoundSentinels records every v2 reference without opening the
// keychain. References bound to this exact source/owner/field/destination are
// left for lazy resolution. Foreign references are cleared in memory and
// quarantined as rejected for every owner, including non-current and orphaned
// entries, so unrelated writes preserve them verbatim instead of becoming
// impossible to repair.
func inventoryBoundSentinels(cfg *Config) {
	if cfg.keychainStates == nil {
		cfg.keychainStates = keychainState{}
	}
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			ref, ok := owner.ref(field)
			if !ok {
				continue
			}
			sentinel := ref.get()
			if !credentials.IsBoundSentinel(sentinel) {
				continue
			}
			binding := owner.binding(field)
			account, ok := credentials.AccountForBoundSentinel(sentinel, binding)
			key := owner.stateKey(field)
			if _, exists := cfg.keychainStates[key]; exists {
				continue
			}
			if !ok {
				ref.set("")
				owner.reject(field, "the keychain reference does not match this config source, owner, field, and destination")
				cfg.keychainStates[key] = keychainFieldState{
					binding: binding, sentinel: sentinel, status: keychainStateRejected,
				}
				continue
			}
			cfg.keychainStates[key] = keychainFieldState{
				binding: binding, account: account, sentinel: sentinel,
				status: keychainStateUnresolved,
			}
		}
	}
}

func normalizeCredentialURL(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = fallback
	}
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return strings.TrimSuffix(raw, "/")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	parsed.Host = host
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if parsed.RawPath != "" {
		parsed.RawPath = strings.TrimSuffix(parsed.RawPath, "/")
	}
	return parsed.String()
}

// contextOwners returns the owners referenced by a single context.
func contextOwners(ctx *Context) []secretOwner {
	var owners []secretOwner
	if ctx.StackEntry != nil {
		owners = append(owners, stackOwner(ctx.Stack, ctx.StackEntry))
	}
	if ctx.CloudEntry != nil {
		owners = append(owners, cloudOwner(ctx.Cloud, ctx.CloudEntry))
	}
	return owners
}

// MarkSecretMutation records an explicit credential set/unset. Most mutations
// are detectable by comparing the loaded plaintext with the value at Write,
// but an unavailable sentinel is represented as an empty string, making an
// explicit unset otherwise indistinguishable from no change.
func (cfg *Config) MarkSecretMutation(ownerName string, field credentials.Field) {
	var source string
	switch {
	case strings.HasPrefix(ownerName, "stack:"):
		if stack := cfg.Stacks[strings.TrimPrefix(ownerName, "stack:")]; stack != nil {
			source = stack.sourceIdentity
		}
	case strings.HasPrefix(ownerName, "cloud:"):
		if entry := cfg.Cloud[strings.TrimPrefix(ownerName, "cloud:")]; entry != nil {
			source = entry.sourceIdentity
		}
	}
	if source == "" {
		source = cfg.sourceIdentity
	}
	if cfg.secretMutations == nil {
		cfg.secretMutations = secretMutationSet{}
	}
	cfg.secretMutations[keychainStateKey{source: source, owner: ownerName, field: field}] = true
}

type secretPathImpact struct {
	owner             string
	secretField       credentials.Field
	destinationFields []credentials.Field
}

func parseSecretPathImpact(path string) secretPathImpact {
	parts := strings.Split(path, ".")
	if len(parts) < 3 {
		return secretPathImpact{}
	}
	var impact secretPathImpact
	switch parts[0] {
	case "stacks":
		impact.owner = credentials.StackOwner(parts[1])
		switch {
		case len(parts) == 4 && parts[2] == "grafana":
			if parts[3] == "server" || parts[3] == "proxy-endpoint" || parts[3] == "tls" {
				impact.destinationFields = []credentials.Field{
					credentials.FieldGrafanaToken,
					credentials.FieldGrafanaPassword,
					credentials.FieldOAuthToken,
					credentials.FieldOAuthRefreshToken,
					credentials.FieldSMToken,
				}
				return impact
			}
			if parts[3] == "user" {
				impact.destinationFields = []credentials.Field{credentials.FieldGrafanaPassword}
				return impact
			}
			impact.secretField = grafanaPathField(parts[3])
		case len(parts) == 5 && parts[2] == "grafana" && parts[3] == "tls":
			impact.destinationFields = []credentials.Field{
				credentials.FieldGrafanaToken,
				credentials.FieldGrafanaPassword,
				credentials.FieldOAuthToken,
				credentials.FieldOAuthRefreshToken,
				credentials.FieldSMToken,
			}
			return impact
		case len(parts) == 5 && parts[2] == "providers" && parts[3] == "synth" && parts[4] == "sm-token":
			impact.secretField = credentials.FieldSMToken
		case len(parts) == 5 && parts[2] == "providers" && parts[3] == "synth" && parts[4] == "sm-url":
			impact.destinationFields = []credentials.Field{credentials.FieldSMToken}
			return impact
		}
	case "cloud":
		impact.owner = credentials.CloudOwner(parts[1])
		if len(parts) == 3 {
			switch parts[2] {
			case "token":
				impact.secretField = credentials.FieldCloudToken
			case "oauth-token":
				impact.secretField = credentials.FieldOAuthToken
			case "api-url", "oauth-url":
				impact.destinationFields = []credentials.Field{
					credentials.FieldCloudToken,
					credentials.FieldOAuthToken,
				}
				return impact
			}
		}
	}
	return impact
}

// PrepareSecretPathMutation captures the exact owner's credential bindings
// before config set/unset mutates it. The returned completion callback must be
// invoked only after the editor mutation succeeds. Normalization-equivalent
// destination edits are no-ops; actual destination changes clear only that
// owner's affected credentials and schedule old generations for post-rename
// deletion.
func (cfg *Config) PrepareSecretPathMutation(path string) func() error {
	impact := parseSecretPathImpact(path)
	if impact.owner == "" {
		return func() error { return nil }
	}
	if impact.secretField != "" {
		return func() error {
			cfg.MarkSecretMutation(impact.owner, impact.secretField)
			return nil
		}
	}
	if len(impact.destinationFields) == 0 {
		return func() error { return nil }
	}

	var before map[credentials.Field]credentials.Binding
	for _, owner := range cfg.secretOwners() {
		if owner.key != impact.owner {
			continue
		}
		if cfg.keychainStore != nil {
			backed, preserve, states := resolveSentinelsForOwner(owner, cfg.keychainStore)
			cfg.trackKeychainResults(backed, preserve, states)
		}
		before = map[credentials.Field]credentials.Binding{}
		for _, field := range impact.destinationFields {
			before[field] = owner.binding(field)
		}
		break
	}
	return func() error {
		if before == nil {
			return nil
		}
		if strings.HasPrefix(impact.owner, "cloud:") {
			if err := cfg.materializeCloudCredentialDestinations(); err != nil {
				return err
			}
		}
		for _, owner := range cfg.secretOwners() {
			if owner.key != impact.owner {
				continue
			}
			for _, field := range impact.destinationFields {
				if before[field] == owner.binding(field) {
					continue
				}
				if ref, ok := owner.ref(field); ok {
					ref.set("")
				}
				cfg.MarkSecretMutation(impact.owner, field)
			}
			return nil
		}
		return nil
	}
}

// MarkSecretPathMutation remains for non-editor callers mutating a credential
// field directly. Destination fields require PrepareSecretPathMutation so the
// old normalized binding is available and are intentionally not invalidated
// by this after-the-fact compatibility helper.
func (cfg *Config) MarkSecretPathMutation(path string) bool {
	impact := parseSecretPathImpact(path)
	if impact.owner == "" || impact.secretField == "" {
		return false
	}
	cfg.MarkSecretMutation(impact.owner, impact.secretField)
	return true
}

func grafanaPathField(name string) credentials.Field {
	switch name {
	case "token":
		return credentials.FieldGrafanaToken
	case "password":
		return credentials.FieldGrafanaPassword
	case "oauth-token":
		return credentials.FieldOAuthToken
	case "oauth-refresh-token":
		return credentials.FieldOAuthRefreshToken
	default:
		return ""
	}
}

// stackFieldRef returns a get/set handle for the named secret on a stack
// entry, or zero-value (ok=false) if the field's parent struct/map is absent.
func stackFieldRef(stack *StackConfig, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldGrafanaToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.APIToken },
			set: func(v string) { stack.Grafana.APIToken = v },
		}, true
	case credentials.FieldGrafanaPassword:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.Password },
			set: func(v string) { stack.Grafana.Password = v },
		}, true
	case credentials.FieldOAuthToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.OAuthToken },
			set: func(v string) { stack.Grafana.OAuthToken = v },
		}, true
	case credentials.FieldOAuthRefreshToken:
		if stack.Grafana == nil {
			return secretRef{}, false
		}
		return secretRef{
			get: func() string { return stack.Grafana.OAuthRefreshToken },
			set: func(v string) { stack.Grafana.OAuthRefreshToken = v },
		}, true
	case credentials.FieldSMToken:
		return providerFieldRef(stack, "synth", "sm-token")
	case credentials.FieldCloudToken:
	}
	return secretRef{}, false
}

// cloudFieldRef returns a get/set handle for the named secret on a cloud auth
// entry.
func cloudFieldRef(entry *CloudEntry, field credentials.Field) (secretRef, bool) {
	switch field {
	case credentials.FieldCloudToken:
		return secretRef{
			get: func() string { return entry.Token },
			set: func(v string) { entry.Token = v },
		}, true
	case credentials.FieldOAuthToken:
		return secretRef{
			get: func() string { return entry.OAuthToken },
			set: func(v string) { entry.OAuthToken = v },
		}, true
	case credentials.FieldGrafanaToken, credentials.FieldGrafanaPassword,
		credentials.FieldOAuthRefreshToken, credentials.FieldSMToken:
	}
	return secretRef{}, false
}

// providerFieldRef returns a get/set handle for stack.Providers[provider][key],
// or zero-value (ok=false) if the provider sub-map has no entry for key.
// The setter creates the parent map on first write so a migration round-trip
// can re-substitute the sentinel value during Write.
func providerFieldRef(stack *StackConfig, provider, key string) (secretRef, bool) {
	if stack.Providers == nil || stack.Providers[provider] == nil {
		return secretRef{}, false
	}
	if _, present := stack.Providers[provider][key]; !present {
		return secretRef{}, false
	}
	return secretRef{
		get: func() string { return stack.Providers[provider][key] },
		set: func(v string) {
			if stack.Providers == nil {
				stack.Providers = map[string]map[string]string{}
			}
			if stack.Providers[provider] == nil {
				stack.Providers[provider] = map[string]string{}
			}
			stack.Providers[provider][key] = v
		},
	}, true
}

// resolveSentinelsForOwner resolves only v2 sentinels matching the exact
// source/owner/field/destination binding derived from the entry being loaded.
// Legacy or foreign sentinels are cleared in memory without any keychain Get,
// then preserved verbatim for an unrelated write. Trusted legacy migration has
// a separate explicit path in migrate.go.
func resolveSentinelsForOwner(owner secretOwner, store credentials.Store) (keychainBacked, keychainPreserved, keychainState) {
	backed, preserve, states := keychainBacked{}, keychainPreserved{}, keychainState{}
	for _, field := range owner.fields {
		ref, ok := owner.ref(field)
		if !ok {
			continue
		}
		cur := ref.get()
		if !credentials.IsSentinel(cur) {
			continue
		}

		binding := owner.binding(field)
		stateKey := owner.stateKey(field)
		if !credentials.MatchesBoundSentinel(cur, binding) {
			ref.set("")
			owner.reject(field, "the keychain reference does not match this config source, owner, field, and destination")
			preserve.mark(owner.key, field, cur)
			states[stateKey] = keychainFieldState{
				binding:  binding,
				sentinel: cur,
				status:   keychainStateRejected,
			}
			continue
		}

		account, ok := credentials.AccountForBoundSentinel(cur, binding)
		if !ok {
			ref.set("")
			owner.reject(field, "the keychain reference is malformed for this credential binding")
			preserve.mark(owner.key, field, cur)
			states[stateKey] = keychainFieldState{binding: binding, sentinel: cur, status: keychainStateRejected}
			continue
		}
		value, err := store.Get(account)
		if err != nil {
			ref.set("")
			if errors.Is(err, credentials.ErrNotFound) {
				owner.reject(field, "the referenced keychain entry does not exist")
				states[stateKey] = keychainFieldState{
					binding:  binding,
					account:  account,
					sentinel: cur,
					status:   keychainStateMissing,
				}
				continue
			}
			owner.reject(field, "the OS keychain could not be read")
			preserve.mark(owner.key, field, cur)
			states[stateKey] = keychainFieldState{
				binding:  binding,
				account:  account,
				sentinel: cur,
				status:   keychainStatePreserved,
			}
			continue
		}
		ref.set(value)
		owner.clearReject(field)
		backed.mark(owner.key, field)
		states[stateKey] = keychainFieldState{
			binding:   binding,
			account:   account,
			sentinel:  cur,
			plaintext: value,
			status:    keychainStateBacked,
		}
	}
	return backed, preserve, states
}

// resolveSentinelsForContext resolves keychain sentinels on the stack and
// cloud entries referenced by a single context. Idempotent: already-resolved
// fields hold plaintext and are skipped.
func resolveSentinelsForContext(ctx *Context, store credentials.Store) (keychainBacked, keychainPreserved, keychainState) {
	backed, preserve, states := keychainBacked{}, keychainPreserved{}, keychainState{}
	for _, owner := range contextOwners(ctx) {
		b, p, s := resolveSentinelsForOwner(owner, store)
		for ownerKey, fields := range b {
			for field := range fields {
				backed.mark(ownerKey, field)
			}
		}
		for ownerKey, fields := range p {
			for field, sentinel := range fields {
				preserve.mark(ownerKey, field, sentinel)
			}
		}
		maps.Copy(states, s)
	}
	return backed, preserve, states
}

// enforceRuntimeCredentialBindings runs after all config overrides. A bound
// value resolved before an endpoint override must not be presented to the new
// destination. Values explicitly supplied by the override (different from the
// resolved plaintext, or marked by ParseEnvIntoContext) are safe to retain.
func enforceRuntimeCredentialBindings(cfg *Config) error {
	if err := validateLocalExternalTLSCredentials(cfg); err != nil {
		return err
	}
	contexts := make([]*Context, 0, len(cfg.Contexts))
	if current := cfg.Contexts[cfg.CurrentContext]; current != nil {
		contexts = append(contexts, current)
	}
	for name, ctx := range cfg.Contexts {
		if name != cfg.CurrentContext {
			contexts = append(contexts, ctx)
		}
	}
	processed := map[keychainStateKey]bool{}
	for _, ctx := range contexts {
		if ctx == nil {
			continue
		}
		for _, owner := range contextOwners(ctx) {
			for _, field := range owner.fields {
				ref, ok := owner.ref(field)
				if !ok {
					continue
				}
				key := owner.stateKey(field)
				if processed[key] {
					continue
				}
				if ownerComesFromLocalLayer(owner) && ctx.runtimeSecretOverrides[field] {
					ref.set("")
					owner.reject(field, "environment credentials cannot be combined with an auto-discovered repository destination")
					processed[key] = true
					continue
				}
				var original credentialOrigin
				if state, ok := cfg.keychainStates[key]; ok && state.status == keychainStateBacked {
					original = credentialOrigin{binding: state.binding, value: state.plaintext}
				} else {
					original = cfg.credentialOrigins[key]
				}
				if !original.binding.Valid() || original.binding == owner.binding(field) {
					if ctx.runtimeSecretOverrides[field] {
						owner.clearReject(field)
					}
					continue
				}
				processed[key] = true
				current := ref.get()
				explicit := ctx.runtimeSecretOverrides[field] || (current != "" && current != original.value)
				if !explicit {
					ref.set("")
					owner.reject(field, "the credential destination changed after configuration overrides")
				} else {
					owner.clearReject(field)
				}
			}
		}
	}
	return nil
}

func validateLocalExternalTLSCredentials(cfg *Config) error {
	seen := map[*StackConfig]bool{}
	validate := func(name string, stack *StackConfig) error {
		if stack == nil || seen[stack] {
			return nil
		}
		seen[stack] = true
		if stack.sourceLayer != "local" || stack.Grafana == nil || stack.Grafana.TLS == nil {
			return nil
		}
		tlsConfig := stack.Grafana.TLS
		if tlsConfig.CertFile != "" || tlsConfig.KeyFile != "" {
			return fmt.Errorf("repository-local stack %q cannot select external TLS client credential files; use --config %s to authorize this file explicitly", name, cfg.Source)
		}
		return nil
	}
	for name, stack := range cfg.Stacks {
		if err := validate(name, stack); err != nil {
			return err
		}
	}
	// ParseEnvIntoContext detaches the selected runtime stack from Config.Stacks.
	// Inspect resolved views as well so process-local TLS overrides retain the
	// same repository trust boundary after that isolation step.
	for name, ctx := range cfg.Contexts {
		if ctx == nil {
			continue
		}
		stackName := ctx.Stack
		if stackName == "" {
			stackName = name
		}
		if err := validate(stackName, ctx.StackEntry); err != nil {
			return err
		}
	}
	return nil
}

func ownerComesFromLocalLayer(owner secretOwner) bool {
	return owner.layer == "local"
}

// hasSecretsToReconcile reports whether Write needs to touch the keychain at
// all. It is true when any secret field holds a value (so it must be written
// through), or when a field is known to be keychain-backed or preserved (so it
// may need a sentinel round-trip or a stale-entry delete). When false, Write
// skips opening the keychain entirely, so secret-less config writes never probe
// the OS backend.
func (cfg *Config) hasSecretsToReconcile() bool {
	if len(cfg.keychainStates) > 0 || len(cfg.secretMutations) > 0 || len(cfg.keychainFields) > 0 || len(cfg.keychainPreserve) > 0 {
		return true
	}
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			if ref, ok := owner.ref(field); ok && ref.get() != "" {
				return true
			}
		}
	}
	return false
}

func (cfg *Config) hasPlaintextSecrets() bool {
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			if ref, ok := owner.ref(field); ok {
				value := ref.get()
				if value != "" && !credentials.IsSentinel(value) {
					return true
				}
			}
		}
	}
	return false
}

// prepareConfigSourceForWrite binds newly-created entries to the target file
// and rejects moving any existing credential-bearing entry between config
// files. A source move must be an explicit export/import operation that obtains
// fresh credentials; ordinary Write must never make another file authoritative
// for an account it did not create.
func prepareConfigSourceForWrite(cfg *Config, target string) error {
	type sourceUpdate struct {
		owner string
		old   string
		set   func(string)
	}
	updates := make([]sourceUpdate, 0, len(cfg.Stacks)+len(cfg.Cloud))
	for _, owner := range cfg.secretOwners() {
		if owner.source == target {
			continue
		}
		if owner.source != "" && ownerCarriesCredential(cfg, owner) {
			return fmt.Errorf(
				"refusing to write credential owner %q from %q to %q; re-authenticate in the target config file",
				owner.key, owner.source, target,
			)
		}
		ownerKey := owner.key
		oldSource := owner.source
		switch {
		case strings.HasPrefix(owner.key, "stack:"):
			stack := cfg.Stacks[strings.TrimPrefix(owner.key, "stack:")]
			updates = append(updates, sourceUpdate{
				owner: ownerKey,
				old:   oldSource,
				set: func(source string) {
					stack.sourceIdentity = source
					stack.sourceLayer = cfg.sourceLayer
				},
			})
		case strings.HasPrefix(owner.key, "cloud:"):
			entry := cfg.Cloud[strings.TrimPrefix(owner.key, "cloud:")]
			updates = append(updates, sourceUpdate{
				owner: ownerKey,
				old:   oldSource,
				set: func(source string) {
					entry.sourceIdentity = source
					entry.sourceLayer = cfg.sourceLayer
				},
			})
		}
	}

	// Apply source changes only after every entry passed the preflight, so an
	// error leaves the caller's pointer-backed entries untouched.
	for _, update := range updates {
		update.set(target)
		for field := range cfg.secretMutationsFor(update.old, update.owner) {
			delete(cfg.secretMutations, keychainStateKey{source: update.old, owner: update.owner, field: field})
			cfg.secretMutations[keychainStateKey{source: target, owner: update.owner, field: field}] = true
		}
	}
	cfg.sourceIdentity = target
	return nil
}

func ownerCarriesCredential(cfg *Config, owner secretOwner) bool {
	for _, field := range owner.fields {
		if ref, ok := owner.ref(field); ok && ref.get() != "" {
			return true
		}
		if _, ok := cfg.keychainStates[owner.stateKey(field)]; ok {
			return true
		}
	}
	return false
}

func (cfg *Config) secretMutationsFor(source, owner string) map[credentials.Field]bool {
	fields := map[credentials.Field]bool{}
	for key := range cfg.secretMutations {
		if key.source == source && key.owner == owner {
			fields[key.field] = true
		}
	}
	return fields
}

type keychainSlot struct {
	owner    secretOwner
	field    credentials.Field
	ref      secretRef
	key      keychainStateKey
	binding  credentials.Binding
	current  string
	state    keychainFieldState
	hasState bool
	dirty    bool
}

type keychainSwap struct {
	ref       secretRef
	plaintext string
}

type keychainStagedWrite struct {
	account       string
	previous      string
	previouslySet bool
	owner         string
	field         credentials.Field
}

type keychainPendingDelete struct {
	account  string
	owner    string
	field    credentials.Field
	previous string
	existed  bool
}

// keychainCommitError reports whether commit restored every old generation it
// may have deleted before a later cleanup failed. When rollbackComplete is
// false, the caller must retain the already-durable new config and its staged
// generations: restoring the old YAML could point it at a missing account.
type keychainCommitError struct {
	err              error
	rollbackComplete bool
}

func (e *keychainCommitError) Error() string { return e.err.Error() }
func (e *keychainCommitError) Unwrap() error { return e.err }

// keychainWriteTransaction stages reversible keychain Sets before the config
// file replacement and defers every destructive Delete until after it. A failed
// encode, chmod, close, or rename rolls Sets back and leaves old accounts intact.
type keychainWriteTransaction struct {
	store               credentials.Store
	log                 logging.Logger
	swaps               []keychainSwap
	writes              []keychainStagedWrite
	deletes             []keychainPendingDelete
	seenDel             map[string]bool
	plaintextFallback   bool
	warnUnavailableOnce func(func())
}

func newKeychainWriteTransaction(store credentials.Store, log logging.Logger) *keychainWriteTransaction {
	return &keychainWriteTransaction{
		store: store, log: log, seenDel: map[string]bool{},
		warnUnavailableOnce: credentials.WarnUnavailableOnce,
	}
}

func (txn *keychainWriteTransaction) swap(ref secretRef, diskValue string) {
	txn.swaps = append(txn.swaps, keychainSwap{ref: ref, plaintext: ref.get()})
	ref.set(diskValue)
}

func (txn *keychainWriteTransaction) restore() {
	for _, item := range txn.swaps {
		item.ref.set(item.plaintext)
	}
}

//nolint:nestif // The error branches distinguish collision, unavailable storage, and fatal keychain failures before any config write.
func (txn *keychainWriteTransaction) stageBoundSet(binding credentials.Binding, value, owner string, field credentials.Field) (credentials.BoundReference, bool, error) {
	const generationAttempts = 4
	for range generationAttempts {
		boundRef, err := credentials.NewBoundReference(binding)
		if err != nil {
			txn.log.Warn("could not generate keychain reference",
				"owner", owner,
				"field", string(field),
				"error", err.Error())
			return credentials.BoundReference{}, false, err
		}
		_, err = txn.store.Get(boundRef.Account)
		if err == nil {
			continue // vanishingly unlikely generation collision
		}
		if errors.Is(err, credentials.ErrNotFound) {
			if err := txn.store.Set(boundRef.Account, value); err != nil {
				if !errors.Is(err, credentials.ErrUnavailable) {
					txn.log.Warn("could not write keychain entry",
						"owner", owner,
						"field", string(field),
						"error", err.Error())
				}
				if errors.Is(err, credentials.ErrUnavailable) {
					return credentials.BoundReference{}, false, nil
				}
				return credentials.BoundReference{}, false, fmt.Errorf("write keychain entry for %q field %q: %w", owner, field, err)
			}
			txn.writes = append(txn.writes, keychainStagedWrite{
				account: boundRef.Account, previouslySet: false,
				owner: owner, field: field,
			})
			return boundRef, true, nil
		}
		if errors.Is(err, credentials.ErrUnavailable) {
			return credentials.BoundReference{}, false, nil
		} else {
			txn.log.Warn("could not inspect keychain entry before write",
				"owner", owner,
				"field", string(field),
				"error", err.Error())
			return credentials.BoundReference{}, false, fmt.Errorf("inspect keychain entry for %q field %q: %w", owner, field, err)
		}
	}
	txn.log.Warn("could not allocate unique keychain reference", "owner", owner, "field", string(field))
	return credentials.BoundReference{}, false, errors.New("could not allocate unique keychain reference")
}

func (txn *keychainWriteTransaction) deferDelete(account, owner string, field credentials.Field) {
	if account == "" || txn.seenDel[account] {
		return
	}
	txn.seenDel[account] = true
	txn.deletes = append(txn.deletes, keychainPendingDelete{account: account, owner: owner, field: field})
}

func (txn *keychainWriteTransaction) rollback() {
	for _, write := range slices.Backward(txn.writes) {
		var err error
		if write.previouslySet {
			err = txn.store.Set(write.account, write.previous)
		} else {
			err = txn.store.Delete(write.account)
		}
		if err != nil && !errors.Is(err, credentials.ErrNotFound) {
			txn.log.Warn("could not roll back keychain entry after config write failure",
				"owner", write.owner,
				"field", string(write.field),
				"error", err.Error())
		}
	}
}

func (txn *keychainWriteTransaction) preflightDeletes() error {
	for i := range txn.deletes {
		pending := &txn.deletes[i]
		value, err := txn.store.Get(pending.account)
		if err == nil {
			pending.previous = value
			pending.existed = true
			continue
		}
		if errors.Is(err, credentials.ErrNotFound) {
			continue
		}
		return fmt.Errorf("cannot verify keychain deletion for %q field %q before writing config: %w", pending.owner, pending.field, err)
	}
	return nil
}

//nolint:nestif // Cleanup failure recovery must restore the failed deletion and every prior deletion in reverse order.
func (txn *keychainWriteTransaction) commit(warningWriter io.Writer) error {
	deleted := make([]keychainPendingDelete, 0, len(txn.deletes))
	for _, pending := range txn.deletes {
		if err := deleteKeychainAccount(txn.store, pending.account, pending.owner, pending.field, txn.log); err != nil {
			rollbackComplete := true
			if pending.existed {
				if restoreErr := txn.store.Set(pending.account, pending.previous); restoreErr != nil {
					rollbackComplete = false
					err = errors.Join(err, fmt.Errorf("restore keychain entry for %q field %q: %w", pending.owner, pending.field, restoreErr))
				}
			}
			for _, prior := range slices.Backward(deleted) {
				if !prior.existed {
					continue
				}
				if restoreErr := txn.store.Set(prior.account, prior.previous); restoreErr != nil {
					rollbackComplete = false
					err = errors.Join(err, fmt.Errorf("restore keychain entry for %q field %q: %w", prior.owner, prior.field, restoreErr))
				}
			}
			return &keychainCommitError{err: err, rollbackComplete: rollbackComplete}
		}
		deleted = append(deleted, pending)
	}
	if txn.plaintextFallback {
		txn.warnUnavailableOnce(func() {
			const message = "credential store unavailable; credentials remain in plaintext on disk"
			const hint = "install or unlock your OS credential store (Keychain, Credential Manager, or Secret Service) to enable encrypted credential storage"
			if warningWriter != nil {
				fmt.Fprintf(warningWriter, "Warning: %s; %s\n", message, hint)
				return
			}
			txn.log.Warn(message, "hint", hint)
		})
	}
	return nil
}

// reconcileKeychain brings the keychain and in-memory config into agreement
// for one YAML encode. It preflights every field before touching the store,
// then temporarily swaps plaintext values for bound v2 sentinels. The returned
// transaction must be committed only after the config file rename succeeds; on
// failure it must be rolled back. In-memory swaps must always be restored.
//
//nolint:gocyclo // Reconciliation exhaustively models each plaintext, bound, missing, preserved, mutated, and deleted secret state.
func reconcileKeychain(cfg *Config, store credentials.Store, log logging.Logger) (*keychainWriteTransaction, error) {
	slots := make(map[keychainStateKey]keychainSlot)
	for _, owner := range cfg.secretOwners() {
		for _, field := range owner.fields {
			ref, ok := owner.ref(field)
			if !ok {
				continue
			}
			key := owner.stateKey(field)
			binding := owner.binding(field)
			current := ref.get()
			state, hasState := cfg.keychainStates[key]
			dirty := cfg.secretMutations[key]
			if hasState && current != state.plaintext {
				dirty = true
			}

			if (current != "" || hasState) && !binding.Valid() {
				return nil, fmt.Errorf("cannot bind credential %q field %q: source or destination is incomplete", owner.key, field)
			}
			if hasState && (state.status == keychainStateBacked || state.status == keychainStateUnresolved || state.status == keychainStatePreserved) &&
				state.binding.Valid() && state.binding != binding && !dirty {
				return nil, fmt.Errorf(
					"credential destination changed for %q field %q; set a new credential before writing",
					owner.key, field,
				)
			}
			if credentials.IsBoundSentinel(current) && !credentials.MatchesBoundSentinel(current, binding) {
				return nil, fmt.Errorf("refusing foreign keychain reference for %q field %q", owner.key, field)
			}

			slots[key] = keychainSlot{
				owner: owner, field: field, ref: ref, key: key, binding: binding,
				current: current, state: state, hasState: hasState, dirty: dirty,
			}
		}
	}

	txn := newKeychainWriteTransaction(store, log)

	// Deleting a stack/cloud entry must delete each bound account that was
	// successfully bound to that exact source and owner. A preserved v2 reference
	// is also safe to remove because its account was derived from the containing
	// entry. Missing and rejected references are never deleted.
	for key, state := range cfg.keychainStates {
		if _, present := slots[key]; present ||
			(state.status != keychainStateBacked && state.status != keychainStateUnresolved && state.status != keychainStatePreserved) {
			continue
		}
		txn.deferDelete(state.account, key.owner, key.field)
	}

	for _, slot := range slots {
		state := slot.state
		current := slot.current
		if slot.hasState && !slot.dirty {
			switch state.status {
			case keychainStateBacked:
				txn.swap(slot.ref, state.sentinel)
				continue
			case keychainStateUnresolved, keychainStatePreserved, keychainStateRejected, keychainStateMissing:
				// A missing keychain generation is still durable evidence that this
				// field was configured with a credential. Preserve that evidence until
				// the user explicitly repairs or removes the field; dropping it during
				// an unrelated write would let the next load fall through to anonymous
				// or empty Basic authentication.
				txn.swap(slot.ref, state.sentinel)
				continue
			}
		}

		if current == "" {
			if slot.hasState && (state.status == keychainStateBacked || state.status == keychainStateUnresolved || state.status == keychainStatePreserved) {
				txn.deferDelete(state.account, slot.owner.key, slot.field)
			}
			continue
		}
		if credentials.IsSentinel(current) {
			// Bound sentinels were checked above. A legacy reference is retained
			// only for the trusted migration path and is never resolved here.
			continue
		}

		boundRef, ok, err := txn.stageBoundSet(slot.binding, current, slot.owner.key, slot.field)
		if err != nil {
			txn.restore()
			txn.rollback()
			return nil, err
		}
		if !ok {
			if slot.hasState {
				txn.restore()
				txn.rollback()
				return nil, fmt.Errorf("cannot replace credential %q field %q while preserving its existing keychain reference: %w", slot.owner.key, slot.field, credentials.ErrUnavailable)
			}
			txn.plaintextFallback = true
			continue
		}
		if slot.hasState && (state.status == keychainStateBacked || state.status == keychainStateUnresolved || state.status == keychainStatePreserved) &&
			state.account != boundRef.Account {
			txn.deferDelete(state.account, slot.owner.key, slot.field)
		}
		txn.swap(slot.ref, boundRef.Sentinel)
	}
	return txn, nil
}

func deleteKeychainAccount(store credentials.Store, account, owner string, field credentials.Field, log logging.Logger) error {
	if account == "" {
		return nil
	}
	if err := store.Delete(account); err != nil && !errors.Is(err, credentials.ErrNotFound) {
		log.Warn("could not remove stale keychain entry",
			"owner", owner,
			"field", string(field),
			"error", err.Error())
		return fmt.Errorf("remove stale keychain entry for %q field %q: %w", owner, field, err)
	}
	return nil
}

// resolveLegacySentinelForMigration is the only helper that may resolve an
// unbound v1-era sentinel. The migrator must supply the owner and field derived
// from the containing legacy node; values embedded in the sentinel are never
// accepted as authority.
var errLegacySentinelMismatch = errors.New("legacy keychain reference does not match its containing field")

func resolveLegacySentinelForMigration(raw, expectedOwner string, expectedField credentials.Field, store credentials.Store) (string, error) {
	owner, field, ok := credentials.ParseSentinel(raw)
	if !ok {
		return "", fmt.Errorf("%w: invalid syntax", errLegacySentinelMismatch)
	}
	if owner != expectedOwner || field != expectedField {
		return "", fmt.Errorf("%w: expected owner %q field %q", errLegacySentinelMismatch, expectedOwner, expectedField)
	}
	return store.Get(credentials.AccountKey(expectedOwner, expectedField))
}

// mergeKeychainRuntime retains runtime state only for the atomic stack/cloud
// entries that won MergeConfigs. This prevents a lower layer's same-named
// credential from being attached to, deleted by, or written through a higher
// layer's replacement entry.
func mergeKeychainRuntime(result *Config, base, over Config) {
	if base.sourceIdentity != over.sourceIdentity {
		result.sourceIdentity = ""
		result.sourceLayer = ""
		result.sourceRevision = [32]byte{}
		result.hasSourceRevision = false
		result.expectSourceAbsent = false
	} else if over.hasSourceRevision {
		result.sourceLayer = over.sourceLayer
		result.sourceRevision = over.sourceRevision
		result.hasSourceRevision = true
		result.expectSourceAbsent = over.expectSourceAbsent
	}
	if result.keychainStore == nil {
		result.keychainStore = over.keychainStore
	}

	active := map[string]bool{}
	for _, owner := range result.secretOwners() {
		active[owner.source+"\x00"+owner.key] = true
	}
	result.keychainStates = keychainState{}
	result.secretMutations = secretMutationSet{}
	result.credentialOrigins = credentialOriginSet{}
	for _, source := range []Config{base, over} {
		for key, state := range source.keychainStates {
			if active[key.source+"\x00"+key.owner] {
				result.keychainStates[key] = state
			}
		}
		for key := range source.secretMutations {
			if active[key.source+"\x00"+key.owner] {
				result.secretMutations[key] = true
			}
		}
		for key, origin := range source.credentialOrigins {
			if active[key.source+"\x00"+key.owner] {
				result.credentialOrigins[key] = origin
			}
		}
	}

	// Keep the compatibility maps coherent for callers and tests that still
	// inspect them; v2 reconciliation uses keychainStates exclusively.
	result.keychainFields = keychainBacked{}
	result.keychainPreserve = keychainPreserved{}
	for key, state := range result.keychainStates {
		switch state.status {
		case keychainStateBacked:
			result.keychainFields.mark(key.owner, key.field)
		case keychainStatePreserved, keychainStateRejected:
			result.keychainPreserve.mark(key.owner, key.field, state.sentinel)
		case keychainStateMissing:
		}
	}
}
