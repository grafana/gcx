package style

// safeFDToInt converts a file descriptor to int with an overflow guard.
func safeFDToInt(fd uintptr) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	if fd > uintptr(maxInt) {
		return 0, false
	}
	//nolint:gosec // guarded by maxInt bounds check above.
	return int(fd), true
}
