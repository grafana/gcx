package telemetry

// K8sReasonOther is the sentinel for a Kubernetes status reason outside the
// fixed vocabulary below. metav1.StatusReason is a plain string type, so a
// server can return anything; anything unlisted travels as this sentinel and
// the raw value never reaches the wire.
const K8sReasonOther = "other"

// k8sReasons is the complete wire vocabulary for Event.K8sReason, apart from
// the K8sReasonOther sentinel. The entries are apimachinery's StatusReason
// WIRE values, not Go identifiers — the distinction matters exactly once, for
// StatusReasonStoreReadError, whose wire value is "StorageReadError".
//
// An empty reason is metav1.StatusReasonUnknown and is deliberately absent: it
// means no reason was found, so the field is omitted rather than sent.
//
//nolint:gochecknoglobals // fixed wire vocabulary, never mutated.
var k8sReasons = map[string]struct{}{
	"Unauthorized":          {},
	"Forbidden":             {},
	"NotFound":              {},
	"AlreadyExists":         {},
	"Conflict":              {},
	"Gone":                  {},
	"Invalid":               {},
	"BadRequest":            {},
	"MethodNotAllowed":      {},
	"NotAcceptable":         {},
	"RequestEntityTooLarge": {},
	"UnsupportedMediaType":  {},
	"Expired":               {},
	"Timeout":               {},
	"ServerTimeout":         {},
	"TooManyRequests":       {},
	"InternalError":         {},
	"ServiceUnavailable":    {},
	"StorageReadError":      {},
}

// K8sReasonLabel maps a raw Kubernetes status reason to the wire vocabulary:
// empty stays empty (the field is omitted), a listed reason passes through
// unchanged, and any other value becomes K8sReasonOther. This is the only
// path from a captured reason to the event, so a server-controlled string can
// never reach the wire verbatim.
func K8sReasonLabel(reason string) string {
	if reason == "" {
		return ""
	}
	if _, ok := k8sReasons[reason]; ok {
		return reason
	}
	return K8sReasonOther
}

// K8sReasonLabels returns every value Event.K8sReason can carry, for tests
// and for validating the vocabulary against the receiver.
func K8sReasonLabels() []string {
	labels := make([]string, 0, len(k8sReasons)+1)
	for reason := range k8sReasons {
		labels = append(labels, reason)
	}
	return append(labels, K8sReasonOther)
}
