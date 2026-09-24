package setup

import "testing"

func TestRuntimeInstallLockPreventsConcurrentBuilds(t *testing.T) {
	root := t.TempDir()
	release, err := acquireRuntimeInstallLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireRuntimeInstallLock(root); err == nil {
		release()
		t.Fatal("a second runtime installer acquired the active lock")
	}
	release()
	releaseAgain, err := acquireRuntimeInstallLock(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseAgain()
}
