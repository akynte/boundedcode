//go:build !linux

package setup

// availableBytes is unknown on platforms without the Linux statfs interface.
// Validation reports the check as skipped rather than inventing a number.
func availableBytes(_ string) (uint64, error) { return 0, nil }
