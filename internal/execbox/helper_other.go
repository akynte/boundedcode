//go:build !linux

package execbox

import "errors"

func runLandlockHelper([]string) error { return errors.New("not supported on this operating system") }
