package telemetry

// ServiceName identifies gcx in the event envelope; the usage-stats receiver
// dispatches on it.
const ServiceName = "gcx"

// Outcome values for Event.Outcome.
const (
	OutcomeOK           = "ok"
	OutcomeRuntimeError = "runtime_error"
	OutcomeParseError   = "parse_error"
	OutcomeHelp         = "help"
	// OutcomeCanceled is any invocation whose final exit code is
	// gcxerrors.ExitCancelled: an interrupt, a declined confirmation prompt, a
	// server-reported cancellation. The event records that the invocation
	// stopped early, never who or what stopped it. Stopping early is not a
	// failure, so ErrorKind stays empty — still on the wire, as for ok and help.
	OutcomeCanceled = "canceled"
)

// Event is the flat wide event describing one gcx invocation. Field names
// follow the usage-stats JSON schema (snake_case); the json encoding of this
// struct is exactly what travels on the wire (see Export).
//
// Privacy invariant, stated as three separate rules because a single blanket
// sentence has twice been written here in a form that was already false:
//
//   - No field carries an argument value, a free-form flag value, a resource
//     name, a hostname, or anything else identifying a person, an organisation,
//     or their data. Flags holds flag NAMES only; Command is the resolved
//     command path only; the parse_error_* fields are shape-filtered before they
//     are set (see #578).
//   - No field carries a raw numeric count. Batch sizes travel as labels from
//     the fixed vocabulary in bucket.go. Note that two of those labels are
//     singletons, so a batch of 0 or 1 is exactly recoverable — say "fixed
//     categories", never "never exact". Numeric fields (ExitCode, DurationMS,
//     HTTPStatus, ParseErrorDistance) describe command execution or protocol
//     behavior, never how much of anything the user has — do not write the
//     rule as "no numbers".
//   - A small, enumerated set of fields is derived from how the command ran
//     rather than from a name, and each is documented on its own field below:
//     OutputFormat (the value of --output, filtered to a fixed list of formats),
//     DryRun (whether the operation ran in dry-run mode), and GrafanaAuthMethod
//     (the authentication category selected for the Grafana connection, clamped
//     to a fixed vocabulary). None says anything about the user, their
//     organisation, or their data.
//
// Do not phrase this as "the only exception is X". That form is a promise about
// every other field in the struct, so it has to be re-audited against the whole
// event each time one is added — and the first version written here claimed
// DryRun was the only one while OutputFormat sat two lines below it. Adding
// another such field means updating the first-run notice (firstrun.go, and
// bumping noticeRevision so existing installs actually see it) and the published
// usage-statistics page.
type Event struct {
	// Envelope.
	Service string `json:"service"`
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`

	// Anonymous install identity.
	DeviceID          string `json:"device_id"`
	DeviceIDPersisted bool   `json:"device_id_persisted"`

	// What ran.
	Command    string `json:"command"`
	Flags      string `json:"flags"`
	Provider   string `json:"provider"`
	Outcome    string `json:"outcome"`
	ExitCode   int    `json:"exit_code"`
	ErrorKind  string `json:"error_kind"`
	DurationMS int64  `json:"duration_ms"`

	// Execution context.
	IsTTY        bool   `json:"is_tty"`
	IsCI         bool   `json:"is_ci"`
	CIProvider   string `json:"ci_provider"`
	IsAgent      bool   `json:"is_agent"`
	Agent        string `json:"agent"`
	TargetKind   string `json:"target_kind"`
	OutputFormat string `json:"output_format"`

	// Batch volume, set only for a batch resource operation that ran to a
	// finalized count. All four are present together or absent together:
	// absent means this invocation was not one of those operations, or it
	// aborted before its counts were final. A failure to render or write the
	// summary afterwards does not clear them — the work still happened.
	//
	// DryRun reports whether the operation ran in dry-run mode. False does not
	// imply that anything was mutated: pull is read-only and always reports
	// false. Interpret it together with Command.
	//
	// The bucket values come from Bucket; units differ per command, so these
	// must be read alongside Command and never summed across commands.
	BatchSucceededBucket *string `json:"batch_succeeded_bucket,omitempty"`
	BatchFailedBucket    *string `json:"batch_failed_bucket,omitempty"`
	BatchSkippedBucket   *string `json:"batch_skipped_bucket,omitempty"`
	DryRun               *bool   `json:"dry_run,omitempty"`

	// Failure depth, set only when the invocation's surfaced error carried
	// one of these two shapes. Both are suppressed for exit code 4 — a
	// partial failure has no single causal status — and exit code 5, because
	// a canceled run is not a failure.
	//
	// HTTPStatus is the HTTP transport status of the failing request, only
	// ever 400–599. It is never a status embedded inside a 2xx response body
	// (a query error inside an HTTP 200 omits this field) and never a
	// Kubernetes Status code — Kubernetes failures are described by K8sReason
	// instead. Coverage is partial: only clients that carry a typed status
	// reach it, so absence never means no HTTP failure occurred.
	//
	// K8sReason is the Kubernetes status reason of the failing API call,
	// clamped to the fixed vocabulary in k8sreason.go. A StatusReason is a
	// plain string a server controls, so any unlisted reason travels as
	// "other", never verbatim.
	HTTPStatus int    `json:"http_status,omitempty"`
	K8sReason  string `json:"k8s_reason,omitempty"`

	// GrafanaAuthMethod is the authentication category the invocation
	// selected for its Grafana connection, clamped to the fixed vocabulary in
	// authmethod.go — never a raw configured value, never credential
	// material. It describes Grafana connection authentication only, not
	// Grafana Cloud/GCOM auth. Absent means no Grafana context was selected,
	// or several different methods were used in one invocation; "anonymous"
	// is a valid selection with no credential material; "unknown" is a
	// selection that could not be classified. A selected method whose
	// credential turns out invalid still reports the method — that failure
	// mode is the field's reason to exist. Eligible on every outcome,
	// including success and canceled.
	GrafanaAuthMethod string `json:"grafana_auth_method,omitempty"`

	// Parse-failure capture, set only when Outcome is OutcomeParseError.
	ParseErrorKind     string `json:"parse_error_kind,omitempty"`
	ParseErrorParent   string `json:"parse_error_parent,omitempty"`
	ParseErrorToken    string `json:"parse_error_token,omitempty"`
	AttemptedCommand   string `json:"attempted_command,omitempty"`
	ParseErrorFlags    string `json:"parse_error_flags,omitempty"`
	ParseErrorNearest  string `json:"parse_error_nearest,omitempty"`
	ParseErrorDistance int    `json:"parse_error_distance,omitempty"`
}
