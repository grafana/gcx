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
)

// Event is the flat wide event describing one gcx invocation. Field names
// follow the usage-stats JSON schema (snake_case); the json encoding of this
// struct is exactly what travels on the wire (see Export).
//
// Privacy invariant: no field may carry argument or flag values, resource
// names, hostnames, or anything else that identifies a person, an
// organisation, or their data. Flags holds flag NAMES only; Command is the
// resolved command path only. The parse_error_* fields are shape-filtered
// before they are set (see #578).
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

	// Parse-failure capture, set only when Outcome is OutcomeParseError.
	ParseErrorKind     string `json:"parse_error_kind,omitempty"`
	ParseErrorParent   string `json:"parse_error_parent,omitempty"`
	ParseErrorToken    string `json:"parse_error_token,omitempty"`
	AttemptedCommand   string `json:"attempted_command,omitempty"`
	ParseErrorFlags    string `json:"parse_error_flags,omitempty"`
	ParseErrorNearest  string `json:"parse_error_nearest,omitempty"`
	ParseErrorDistance int    `json:"parse_error_distance,omitempty"`
}
