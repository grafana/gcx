package adapter

// ResourceIdentity provides self-describing identity for provider domain types.
// Every domain type used in a ResourceAdapter must implement this interface so
// that TypedCRUD can extract and restore resource names without function pointers.
//
// Pointer types (*Slo, *Probe, etc.) satisfy this full interface because
// SetResourceName requires a pointer receiver to mutate the identity field.
// Use compile-time assertions (var _ ResourceIdentity = &MyType{}) to verify.
type ResourceIdentity interface {
	// GetResourceName returns the resource's identity as a string.
	// For types with string identifiers, return the field directly.
	// For types with numeric identifiers, convert via strconv.
	GetResourceName() string

	// SetResourceName restores the identity from a string (e.g., after K8s
	// round-trip via metadata.name). For numeric types, parse errors are
	// silently ignored.
	SetResourceName(name string)
}

// ResourceNamer is the value-type-compatible subset of ResourceIdentity used
// as the TypedCRUD type constraint. Go generics cannot enforce pointer-receiver
// methods (SetResourceName) on value types, so TypedCRUD constrains on
// GetResourceName() only. SetResourceName is accessed via type assertion on *T.
//
// All domain types that implement ResourceIdentity also satisfy ResourceNamer.
type ResourceNamer interface {
	GetResourceName() string
}
