package output

// Discriminators for the agent-mode stream envelope shared by every
// stream-class command (docs/design/agent-mode.md §6.4). In agent mode each
// JSONL line a stream-class command writes carries one of these `type` tags
// plus StreamSchemaVersion, and the stream terminates with exactly one
// StreamEndType line reporting the outcome.
const (
	// StreamEventType tags each streamed progress/domain event line.
	StreamEventType = "gcx.stream_event"
	// StreamEndType tags the terminal success/error line of a stream.
	StreamEndType = "gcx.stream_end"
	// StreamSchemaVersion versions the stream envelope shape itself (the
	// discriminator wrapper), not the domain payload fields it carries.
	StreamSchemaVersion = "1"
)
