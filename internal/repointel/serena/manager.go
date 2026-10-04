package serena

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/akynte/boundedcode/internal/repointel"
)

// Manager owns Serena processes: at most one per checkout root, at most
// MaxInstances at a time (least recently used idle instances are stopped to
// make room), and none idle for longer than IdleTimeout. Serena's MCP server
// holds one active project, so instances are never switched between
// projects; a different root means a different instance (ADR-0008).
//
// A Manager is safe for concurrent use. Calls to one instance are
// serialized by Serena itself.
type Manager struct {
	Executable string
	// Root holds instance homes (configuration and symbol caches).
	Root string
	// LanguageServers is the shared directory of language servers Serena
	// installs itself (TypeScript); it belongs to the installation and is
	// populated by `serena setup`. Default: Root/language_servers.
	LanguageServers string
	// LogDir receives one log per instance.
	LogDir         string
	MaxInstances   int
	IdleTimeout    time.Duration
	StartupTimeout time.Duration
	// CallTimeout bounds one tool call; a call that exceeds it kills the
	// instance (a hung language server blocks every later call).
	CallTimeout time.Duration
	Log         *slog.Logger
	// SkipVersionCheck is for tests with a fake server only.
	SkipVersionCheck bool
	// ExtraEnv is appended to the server environment (tests).
	ExtraEnv []string
	// editing enables symbol-level editing tools. Only the Stage 2
	// experiment sets it; there is deliberately no exported way to do so.
	editing bool

	mu        sync.Mutex
	sessions  map[string]*session
	starting  map[string]chan struct{}
	failures  map[string]failure
	verified  error // result of the one-time version check
	checked   bool
	reaper    chan struct{}
	closed    bool
	stats     Stats
	languages map[string]Project
}

type failure struct {
	count int
	until time.Time
}

// Stats are cumulative counters for observability and benchmarks.
type Stats struct {
	Starts        int           `json:"starts"`
	StartFailures int           `json:"start_failures"`
	Restarts      int           `json:"restarts"` // starts that replaced a dead instance
	Evictions     int           `json:"evictions"`
	IdleStops     int           `json:"idle_stops"`
	Calls         int           `json:"calls"`
	CallErrors    int           `json:"call_errors"`
	Timeouts      int           `json:"timeouts"`
	StartTime     time.Duration `json:"start_time"`
	CallTime      time.Duration `json:"call_time"`
}

// Instance describes a running instance.
type Instance struct {
	Root     string    `json:"root"`
	PID      int       `json:"pid"`
	Started  time.Time `json:"started"`
	LastUsed time.Time `json:"last_used"`
}

// breaker: after this many consecutive start failures for a root, Serena is
// not retried for that root until the cooldown passes. Callers fall back.
const (
	breakerThreshold = 2
	breakerCooldown  = 5 * time.Minute
)

func (m *Manager) defaults() {
	if m.MaxInstances <= 0 {
		m.MaxInstances = 2
	}
	if m.IdleTimeout <= 0 {
		m.IdleTimeout = 10 * time.Minute
	}
	if m.StartupTimeout <= 0 {
		m.StartupTimeout = 90 * time.Second
	}
	if m.CallTimeout <= 0 {
		m.CallTimeout = 30 * time.Second
	}
	if m.Log == nil {
		m.Log = slog.New(slog.DiscardHandler)
	}
	if m.sessions == nil {
		m.sessions, m.starting, m.failures, m.languages = map[string]*session{}, map[string]chan struct{}{}, map[string]failure{}, map[string]Project{}
	}
}

// Verify checks the installed version once per Manager.
func (m *Manager) Verify(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults()
	return m.verifyLocked(ctx)
}

func (m *Manager) verifyLocked(ctx context.Context) error {
	if m.checked {
		return m.verified
	}
	m.checked = true
	if m.SkipVersionCheck {
		return nil
	}
	inst, err := Detect(ctx, m.Executable)
	if err == nil {
		err = Check(inst)
	}
	m.verified = err
	if err != nil {
		m.Log.Warn("serena disabled", "err", err)
	}
	return err
}

// Register sets the project definition (languages, ignored paths) used when
// an instance for root is started. Unregistered roots are served with the
// languages detected by the caller of Ensure.
func (m *Manager) Register(p Project) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults()
	m.languages[canonical(p.Root)] = p
}

// Ensure returns a running instance for the project root, starting one if
// needed.
func (m *Manager) Ensure(ctx context.Context, p Project) (*session, error) {
	root := canonical(p.Root)
	p.Root = root
	for {
		m.mu.Lock()
		m.defaults()
		if m.closed {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: serena manager closed", repointel.ErrUnavailable)
		}
		if err := m.verifyLocked(ctx); err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %w", repointel.ErrUnavailable, err)
		}
		if s := m.sessions[root]; s != nil {
			if s.alive() {
				s.touch()
				m.mu.Unlock()
				return s, nil
			}
			// Crashed since last use: drop it and start a new one.
			delete(m.sessions, root)
			m.stats.Restarts++
			go s.kill()
		}
		if f := m.failures[root]; f.count >= breakerThreshold && time.Now().Before(f.until) {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: serena failed to start %d times for %s; retrying after %s",
				repointel.ErrUnavailable, f.count, root, f.until.Format(time.TimeOnly))
		}
		if wait, ok := m.starting[root]; ok {
			m.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if reg, ok := m.languages[root]; ok {
			p = reg
			p.Root = root
		}
		if len(p.Languages) == 0 {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: no language supported by serena in %s", repointel.ErrUnavailable, root)
		}
		victims := m.evictForLocked()
		wait := make(chan struct{})
		m.starting[root] = wait
		m.startReaperLocked()
		m.mu.Unlock()

		for _, v := range victims {
			v.close()
		}
		s, err := m.start(ctx, p)

		m.mu.Lock()
		delete(m.starting, root)
		close(wait)
		if err != nil {
			m.stats.StartFailures++
			f := m.failures[root]
			f.count++
			f.until = time.Now().Add(breakerCooldown)
			m.failures[root] = f
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %w", repointel.ErrUnavailable, err)
		}
		delete(m.failures, root)
		if m.closed {
			m.mu.Unlock()
			s.close()
			return nil, fmt.Errorf("%w: serena manager closed", repointel.ErrUnavailable)
		}
		m.sessions[root] = s
		m.mu.Unlock()
		return s, nil
	}
}

func (m *Manager) start(ctx context.Context, p Project) (*session, error) {
	t0 := time.Now()
	l, err := prepareHome(m.Root, m.LanguageServers, p, m.editing)
	if err != nil {
		return nil, err
	}
	key := instanceKey(p.Root)
	if n := sweepOrphans(key); n > 0 {
		m.Log.Warn("killed orphaned serena processes from a previous run", "root", p.Root, "count", n)
	}
	logPath := filepath.Join(m.LogDir, "serena-"+key+".log")
	s, err := start(ctx, startSpec{Executable: m.Executable, Layout: l, Project: p, LogPath: logPath,
		Timeout: m.StartupTimeout, Env: m.ExtraEnv, Tag: instanceTag(key)})
	m.mu.Lock()
	m.stats.Starts++
	m.stats.StartTime += time.Since(t0)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.Log.Debug("serena started", "root", p.Root, "pid", s.pid(), "languages", p.Languages, "seconds", time.Since(t0).Seconds())
	return s, nil
}

// evictForLocked picks idle instances to stop so a new one fits.
func (m *Manager) evictForLocked() []*session {
	var idle []*session
	for _, s := range m.sessions {
		if s.idle() {
			idle = append(idle, s)
		}
	}
	sort.Slice(idle, func(i, j int) bool { return idle[i].lastUse().Before(idle[j].lastUse()) })
	var out []*session
	for len(m.sessions) >= m.MaxInstances && len(idle) > 0 {
		v := idle[0]
		idle = idle[1:]
		delete(m.sessions, v.root)
		m.stats.Evictions++
		out = append(out, v)
	}
	return out
}

// Call runs one tool on the instance for p.Root, bounded by CallTimeout and
// ctx. A timeout or transport failure stops the instance; the next call
// starts a fresh one.
func (m *Manager) Call(ctx context.Context, p Project, tool string, args map[string]any) (string, error) {
	s, err := m.Ensure(ctx, p)
	if err != nil {
		return "", err
	}
	s.acquire()
	defer s.release()
	cctx, cancel := context.WithTimeout(ctx, m.CallTimeout)
	defer cancel()
	t0 := time.Now()
	out, err := s.call(cctx, tool, args)
	m.mu.Lock()
	m.stats.Calls++
	m.stats.CallTime += time.Since(t0)
	if err != nil {
		m.stats.CallErrors++
	}
	m.mu.Unlock()
	var ce *errCall
	switch {
	case err == nil, errors.As(err, &ce):
		return out, err
	case ctx.Err() != nil:
		// The caller gave up (task cancelled). Serena keeps executing the
		// tool, so stop the instance rather than leave a busy LSP behind.
		m.drop(s)
		return "", ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		m.mu.Lock()
		m.stats.Timeouts++
		m.mu.Unlock()
		m.drop(s)
		return "", fmt.Errorf("%w: serena %s timed out after %s; instance stopped", repointel.ErrUnavailable, tool, m.CallTimeout)
	default:
		m.drop(s)
		return "", fmt.Errorf("%w: serena %s: %w%s", repointel.ErrUnavailable, tool, err, s.exitDetail())
	}
}

// drop removes and kills an instance.
func (m *Manager) drop(s *session) {
	m.mu.Lock()
	if m.sessions[s.root] == s {
		delete(m.sessions, s.root)
	}
	m.mu.Unlock()
	go s.kill()
}

// Health checks a root's instance without starting one.
func (m *Manager) Health(ctx context.Context, root string) error {
	m.mu.Lock()
	m.defaults()
	s := m.sessions[canonical(root)]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no serena instance for %s", root)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.ping(ctx)
}

// Stop stops a root's instance, if any.
func (m *Manager) Stop(root string) {
	m.mu.Lock()
	m.defaults()
	s := m.sessions[canonical(root)]
	delete(m.sessions, canonical(root))
	m.mu.Unlock()
	if s != nil {
		s.close()
	}
}

// Forget stops a root's instance and deletes its cache and configuration.
func (m *Manager) Forget(root string) error {
	m.Stop(root)
	return removeHome(m.Root, canonical(root))
}

// Close stops every instance. The Manager cannot be used afterwards.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.defaults()
	m.closed = true
	all := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*session{}
	if m.reaper != nil {
		close(m.reaper)
		m.reaper = nil
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Go(s.close)
	}
	wg.Wait()
	return nil
}

// Instances lists running instances.
func (m *Manager) Instances() []Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaults()
	out := make([]Instance, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, Instance{Root: s.root, PID: s.pid(), Started: s.started, LastUsed: s.lastUse()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Root < out[j].Root })
	return out
}

// Stats returns cumulative counters.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

func (m *Manager) startReaperLocked() {
	if m.reaper != nil {
		return
	}
	stop := make(chan struct{})
	m.reaper = stop
	interval := max(m.IdleTimeout/4, 50*time.Millisecond)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				m.reapIdle()
			}
		}
	}()
}

func (m *Manager) reapIdle() {
	m.mu.Lock()
	var victims []*session
	for root, s := range m.sessions {
		if s.idle() && time.Since(s.lastUse()) > m.IdleTimeout {
			delete(m.sessions, root)
			m.stats.IdleStops++
			victims = append(victims, s)
		}
	}
	m.mu.Unlock()
	for _, s := range victims {
		m.Log.Debug("serena idle stop", "root", s.root)
		s.close()
	}
}

func (s *session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *session) acquire() {
	s.mu.Lock()
	s.inUse++
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *session) release() {
	s.mu.Lock()
	s.inUse--
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *session) idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inUse == 0
}

func (s *session) lastUse() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// canonical resolves symlinks so two spellings of one worktree share an
// instance and an instance never silently serves a different directory.
func canonical(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return filepath.Clean(root)
}

// Usage sums the resource use of all running instances (Linux; zero
// elsewhere). Language servers are included.
func (m *Manager) Usage() Usage {
	m.mu.Lock()
	tags := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		tags = append(tags, s.tag)
	}
	m.mu.Unlock()
	var total Usage
	for _, t := range tags {
		u := usageOf(t)
		total.Processes += u.Processes
		total.RSSMiB += u.RSSMiB
		total.CPUSeconds += u.CPUSeconds
	}
	return total
}
