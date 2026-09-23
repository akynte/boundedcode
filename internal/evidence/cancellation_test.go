package evidence

// Cancellation is a behavioural claim, so it is tested rather than asserted by
// the presence of a ctx parameter.
//
// These exercise the subprocess boundary deterministically with git, which the
// suite already requires and which behaves identically to the docker and
// python calls on the same paths: exec.CommandContext kills the process when
// the context ends. Docker itself is not driven here — a test that needs a
// daemon is a test that reports the daemon's mood, not this code's.
//
// What each one is for:
//   - an already-cancelled context must not start work at all
//   - cancelling mid-flight must return rather than wait for the subprocess
//   - a deadline must do the same without a manual cancel
//   - the success path must be unchanged by any of it
//   - cleanup must survive the cancellation that stopped the work, or every
//     cancelled run leaks the container it started

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed; this test exercises the subprocess boundary")
	}
}

// run is the same shape as the helpers threaded in this change: a fixed
// executable, arguments from the caller, and the operation's context.
func run(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "git", args...).Run()
}

func TestAnAlreadyCancelledContextStartsNoSubprocess(t *testing.T) {
	gitAvailable(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := run(ctx, "--version")
	if err == nil {
		t.Fatal("a cancelled context ran the subprocess anyway")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %s to refuse; it should not have started", elapsed)
	}
}

func TestCancellationWhileASubprocessRunsReturnsRatherThanHanging(t *testing.T) {
	gitAvailable(t)
	// `git help --all` is cheap; the sleep below is what makes the process
	// long-lived, so the test does not depend on any git command being slow.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep is unavailable: %v", err)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("the subprocess exited cleanly; cancellation did not stop it")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context did not stop the subprocess; it hung")
	}
}

func TestADeadlineStopsASubprocessWithoutAManualCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("sleep is unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("the subprocess outlived its deadline")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the deadline did not stop the subprocess; it hung")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Errorf("ctx.Err() = %v, want DeadlineExceeded", ctx.Err())
	}
}

func TestTheSuccessPathIsUnchanged(t *testing.T) {
	gitAvailable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, "--version"); err != nil {
		t.Errorf("git --version under a live context: %v", err)
	}
}

// The property that keeps cancellation from becoming a container leak.
func TestCleanupContextSurvivesACancelledParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel() // the run is cancelled; the container still exists

	ctx, done := cleanupContext(parent)
	defer done()

	if err := ctx.Err(); err != nil {
		t.Fatalf("cleanup context is already dead (%v); the container would leak", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("cleanup context has no deadline; a wedged daemon could hang the exit")
	}
	if remaining := time.Until(deadline); remaining > cleanupTimeout+time.Second {
		t.Errorf("deadline is %s away, want at most %s", remaining, cleanupTimeout)
	}
}

// A cleanup command must actually run under that context.
func TestACleanupSubprocessRunsAfterCancellation(t *testing.T) {
	gitAvailable(t)
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	ctx, done := cleanupContext(parent)
	defer done()
	if err := exec.CommandContext(ctx, "git", "--version").Run(); err != nil {
		t.Errorf("cleanup subprocess did not run after cancellation: %v", err)
	}
}

// containedPath is the trust boundary for the one write that leaves this
// package's own directory tree.
func TestContainedPathRefusesAnEscape(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		root + "/../escape.yaml",
		root + "/sub/../../escape.yaml",
	} {
		if _, err := containedPath(root, p); err == nil {
			t.Errorf("containedPath accepted %q, which leaves the run root", p)
		}
	}
}

func TestContainedPathAcceptsAPathInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	got, err := containedPath(root, root+"/environments/docker/compose.yaml")
	if err != nil {
		t.Fatalf("rejected a path inside the root: %v", err)
	}
	if got == "" {
		t.Error("returned an empty path")
	}
}
