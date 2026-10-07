//go:build !windows

package config

// restrictToOwner is a no-op where file modes already restrict access.
func restrictToOwner(string, bool) error { return nil }
