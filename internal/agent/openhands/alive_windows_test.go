//go:build windows

package openhands

import "github.com/akynte/boundedcode/internal/proctree"

func processAlive(pid int) bool { return proctree.Alive(pid) }
