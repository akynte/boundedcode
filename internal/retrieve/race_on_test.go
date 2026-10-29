//go:build race

package retrieve

// raceEnabled: the race detector slows this package down a hundredfold,
// so wall-clock bounds are not checked under it.
const raceEnabled = true
