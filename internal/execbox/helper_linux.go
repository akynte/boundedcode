//go:build linux

package execbox

func runLandlockHelper(argv []string) error { return runLandlock(argv) }
