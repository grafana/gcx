package fleet

// ErrPipelineManagedByInstrumentationForTest exposes the internal guard error
// constructor so that external (fleet_test) test files can reconstruct the
// same error value and assert on its concrete type.
func ErrPipelineManagedByInstrumentationForTest(name string) error {
	return errPipelineManagedByInstrumentation(name)
}
