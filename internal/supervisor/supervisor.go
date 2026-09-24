// Package supervisor assembles the task runner that governs a piece of work.
//
// A runner is not a constructor call. It carries the sandbox verification runs
// in, the repository's protected paths, the gate policy, the analyzers a
// freshness check re-runs, and the environment a build is given — and every one
// of those is a control rather than a setting. A second assembly that forgot the
// sandbox would still compile, still run, and would run unconfined.
//
// It lived in cmd/bcode, which meant the completion contract was reachable only
// from a terminal. That was fine while the CLI was the only interface. It stopped
// being fine when an editor needed to put work under the same contract: the
// choice was to duplicate two hundred lines of security-relevant wiring, or to
// move it somewhere both callers could reach. This is the second.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/akynte/boundedcode/internal/analyzers/architecture"
	"github.com/akynte/boundedcode/internal/analyzers/deploy"
	"github.com/akynte/boundedcode/internal/analyzers/gitlog"
	"github.com/akynte/boundedcode/internal/analyzers/golang"
	"github.com/akynte/boundedcode/internal/analyzers/protoavro"
	"github.com/akynte/boundedcode/internal/analyzers/python"
	sqlan "github.com/akynte/boundedcode/internal/analyzers/sql"
	"github.com/akynte/boundedcode/internal/analyzers/terraform"
	"github.com/akynte/boundedcode/internal/analyzers/typescript"
	"github.com/akynte/boundedcode/internal/attest"
	"github.com/akynte/boundedcode/internal/broker"
	"github.com/akynte/boundedcode/internal/config"
	"github.com/akynte/boundedcode/internal/critic"
	"github.com/akynte/boundedcode/internal/engine"
	"github.com/akynte/boundedcode/internal/index"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/llm"
	"github.com/akynte/boundedcode/internal/oracle"
	"github.com/akynte/boundedcode/internal/policy"
	"github.com/akynte/boundedcode/internal/recipe"
	"github.com/akynte/boundedcode/internal/sandbox"
	"github.com/akynte/boundedcode/internal/sandbox/bwrap"
	"github.com/akynte/boundedcode/internal/sandbox/landlock"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/task"
)

// Options are what differs between callers. Everything else is loaded here, so
// two interfaces cannot end up with two different sets of controls.
type Options struct {
	// RepoRoot is the repository whose policies apply and whose graph a
	// freshness check re-analyses.
	RepoRoot string
	// Logf reports progress. Nil discards it.
	Logf func(format string, args ...any)
	// Warnf reports analyzer problems. Nil discards them.
	Warnf func(format string, args ...any)
	// OracleDir overrides the configured hidden acceptance suite.
	OracleDir string
}

// Runner builds a task runner with every control in place.
//
// It refuses rather than degrades when no sandbox is available. Verification
// runs commands chosen by a model over the operator's code; running those
// unconfined because confinement was unavailable is the one outcome worth
// failing for.
func Runner(ctx context.Context, root *store.Root, st *store.Store, eng engine.Engine, o Options) (*task.Runner, error) {
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	warnf := o.Warnf
	if warnf == nil {
		warnf = func(string, ...any) {}
	}

	cfg, err := Config(root)
	if err != nil {
		return nil, fmt.Errorf("load boundedcode configuration: %w", err)
	}
	sb, report := SelectSandbox(ctx, cfg)
	if sb == nil {
		return nil, fmt.Errorf("no sandbox runner is available; verification must not run unconfined")
	}
	logf("sandbox: %s (%v)", report.Runner, report.Active)

	holder, _ := os.Hostname()
	r, err := task.NewRunner(st, eng, sb, fmt.Sprintf("%s/%d", holder, os.Getpid()))
	if err != nil {
		return nil, err
	}
	r.Logf = logf
	r.Broker = broker.New(st, GatePolicy(cfg.Gates))

	// Repository-wide rules (§6.2), loaded from the repository being worked on
	// rather than from the data directory: a rule about what may not change
	// belongs beside the thing it protects.
	policies, err := policy.Load(filepath.Join(o.RepoRoot, "policies"))
	if err != nil {
		return nil, fmt.Errorf("the repository's policies are invalid: %w", err)
	}
	if len(policies.Policies) > 0 {
		logf("policies: %d rule(s) protecting %d path pattern(s)",
			len(policies.Policies), len(policies.Paths()))
	}
	r.Policies = policies

	suite, err := loadOracle(firstNonEmpty(o.OracleDir, cfg.Oracle.Dir), o.RepoRoot)
	if err != nil {
		return nil, err
	}
	if suite != nil {
		logf("oracle: %d hidden acceptance check(s) from %s (digest %.12s)",
			len(suite.Checks), suite.Dir, suite.Digest)
	}
	r.Oracle = suite
	r.HiddenFeedbackRounds = cfg.Oracle.FeedbackRounds

	// Every verification run is appended to the evidence chain and signed
	// with the data directory's verifier key, created on first use. A runner
	// that cannot sign refuses to start: unsigned evidence would look like
	// the signed kind to anyone who does not check.
	key, err := attest.LoadOrCreate(root.Layout().KeysDir())
	if err != nil {
		return nil, fmt.Errorf("the verifier signing key: %w", err)
	}
	r.Ledger.SetSigner(key)

	activeProfile, err := ProfileChecked(root, cfg)
	if err != nil {
		return nil, fmt.Errorf("load active profile: %w", err)
	}
	if activeProfile != nil {
		r.PhaseBudgets = activeProfile.PhaseBudgets
	}

	// §3.4: a repository the watcher has marked dirty is re-analysed before a
	// step consults the graph, with the analyzers a full index would use.
	r.Freshener = index.New(st, index.Options{
		MaxFileBytes: cfg.Index.MaxFileBytes,
		Excludes:     cfg.Index.Excludes,
		ChunkLines:   cfg.Index.ChunkLines,
		Analyzers:    Analyzers(warnf),
		Semantic:     Semantic(warnf),
	})

	// §10.1's out-of-conversation calls need structured output, so a provider
	// that cannot constrain its answers does not get them: DR-4 refuses rather
	// than degrading, and a review parsed out of prose loses concerns silently.
	// The structured phases follow the planning role rather than whatever
	// model the engine happens to hold. task.NewRunner seeds WorkflowModel
	// from the engine so a standalone engine still works; this replaces it
	// when providers.yaml routes planning somewhere of its own.
	planningProvider, err := PlanningProvider(root)
	if err != nil {
		return nil, fmt.Errorf("resolve planning role: %w", err)
	}
	if provider := planningProvider; provider != nil {
		if !provider.Capabilities().StructuredOutput {
			return nil, fmt.Errorf("the planning role is routed to %s, which does not declare "+
				"structured output; LOCALIZE and PLAN are structured calls and a plan parsed "+
				"out of prose loses obligations silently", provider.Name())
		}
		if r.WorkflowModel == nil || provider.Name() != r.WorkflowModel.Name() {
			logf("planning: %s", provider.Name())
		}
		r.WorkflowModel = provider
	}

	reviewProvider, err := ReviewProvider(root)
	if err != nil {
		return nil, fmt.Errorf("resolve review role: %w", err)
	}
	if provider := reviewProvider; provider != nil && provider.Capabilities().StructuredOutput {
		r.ReviewModel = provider
		r.Critic = &critic.Critic{Provider: provider, MaxTokens: 2048, Temperature: 0.1}
		if activeProfile != nil {
			r.Critic.Thinking = activeProfile.Thinking
			r.Critic.MaxTokens = activeProfile.ReservedOutput
		}
	} else if provider != nil {
		logf("review and diagnosis are off: %s does not declare structured output", provider.Name())
	}

	// The decision plane. It is a required runtime component: a judge that is
	// off or unconfigured does not degrade task execution to a weaker mode,
	// it stops it — see judgment.Readiness.Blocking, which Runner.Run calls
	// before doing any expensive work.
	//
	// Constructing one is still not an error here. Every command that is not
	// a task run — `bcode doctor` above all — has to keep working precisely when
	// the plane is broken, because that is when an operator needs it.
	judge, err := Judge(root, st, logf)
	if err != nil {
		return nil, err
	}
	r.Judge = judge
	r.Retriever = r.Retriever.WithJudge(judge, logf)
	// Calibration is wired whenever a workspace store exists, independent of
	// whether a judge is configured: predictions from a run made before
	// judgments were turned on are not retroactively creatable, but a store
	// with no judge simply never has anything written to it, and readiness
	// for the day it does costs nothing.
	if st != nil {
		r.Calibration = ledger.NewCalibrationStore(st)
		// The denominator for every site-level rate. Wired on the same
		// condition and for the same reason: observation that only exists
		// once a judge is configured cannot describe the runs that had none.
		r.Consultations = &observer{s: ledger.NewConsultationStore(st)}
	}

	dirs, err := st.TaskDirs()
	if err != nil {
		return nil, err
	}
	r.SandboxSpec = BaseSandboxSpec(cfg, dirs)
	// The pool is built by the runner, which is where the repository root is
	// known. Rooted there rather than at a task worktree: a server indexes a
	// project once, and a per-task checkout would pay that cost every task.
	r.LSPConfig = cfg.LSP
	return r, nil
}

// BaseSandboxSpec is the confinement every child process of a workspace starts
// from: the operator's read-only toolchain paths, the workspace's own tmp and
// caches, and the TCP grants a task legitimately needs.
//
// It is one function rather than a literal at each call site because a second
// copy is how a confined path ends up granted in one place and denied in the
// other, and the difference only shows when someone is already looking.
func BaseSandboxSpec(cfg config.Config, dirs store.TaskDirs) sandbox.Spec {
	spec := sandbox.Spec{
		ReadOnly: cfg.Sandbox.ReadOnlyPaths,
		TmpDir:   dirs.Tmp,
		Env:      recipe.GoEnv(dirs.GoBuildCache, dirs.GoModCache, dirs.Tmp),
		// A test suite binds port 0 and connects to whatever the kernel
		// returns, so no allowlist can name those ports in advance. The range
		// holds no services, and TCPDeny keeps it that way.
		AllowEphemeralTCP: true,
		TCPDeny:           ServicePorts(cfg),
	}
	for _, port := range cfg.Sandbox.AllowedTCPConnect {
		spec.TCPConnect = append(spec.TCPConnect, uint16(port)) //nolint:gosec // operator-configured port
	}
	if cfg.Inference.Mode == config.ModeEmbedded {
		spec.TCPConnect = append(spec.TCPConnect, uint16(cfg.Inference.Port)) //nolint:gosec // operator-configured port
	}
	return spec
}

// Config loads the operator configuration.
func Config(root *store.Root) (config.Config, error) {
	return config.Load(root.Layout().ConfigDir())
}

// Profile loads the active hardware profile, or nil when none is set or it
// cannot be read. A missing profile is a degraded default, not a failure.
func Profile(root *store.Root, cfg config.Config) *config.Profile {
	p, _ := ProfileChecked(root, cfg)
	return p
}

// ProfileChecked is the fail-closed form used by task construction. Profile is
// retained for doctor/setup callers that intentionally treat a missing or
// unreadable optional profile as the shipped default.
func ProfileChecked(root *store.Root, cfg config.Config) (*config.Profile, error) {
	if cfg.Profile == "" {
		return nil, nil
	}
	p, err := config.LoadProfile(filepath.Join(root.Layout().ConfigDir(), "profiles"), cfg.Profile)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SelectSandbox picks the strongest confinement the host actually permits.
func SelectSandbox(ctx context.Context, cfg config.Config) (sandbox.Runner, sandbox.Report) {
	var candidates []sandbox.Runner
	ll, llErr := landlock.New()
	switch cfg.Sandbox.Mode {
	case "none":
	case "landlock":
		if llErr == nil {
			candidates = append(candidates, ll)
		}
	case "bwrap":
		if llErr == nil {
			candidates = append(candidates, bwrap.New(ll))
		}
	default:
		if llErr == nil {
			candidates = append(candidates, bwrap.New(ll), ll)
		}
	}
	candidates = append(candidates, sandbox.ContainerRunner{})
	return sandbox.Select(ctx, candidates)
}

// GatePolicy turns gate configuration into the broker's policy.
func GatePolicy(g config.GateConfig) broker.Policy {
	return broker.Policy{
		RequireForBreaking:   g.Breaking,
		RequireForOutOfScope: g.OutOfScope,
		RequireForApply:      g.Apply,
		RequireForPlan:       g.Plan,
		Timeout:              time.Duration(g.TimeoutMinutes) * time.Minute,
	}
}

// ReviewProvider returns the provider routed to the review role, or nil when
// none is configured. No providers is not a fault: review is an addition.
func ReviewProvider(root *store.Root) (llm.Provider, error) {
	return RoleProvider(root, llm.RoleReview)
}

// PlanningProvider returns the provider routed to the planning role.
//
// PLAN and EDIT were the same model for as long as the workflow model was
// taken from the engine, which made "which model plans" unanswerable in
// configuration. They are different jobs — one writes a plan as structured
// output, the other drives a tool loop — and a machine that cannot hold two
// generation models at once still has to be able to say which one does which.
func PlanningProvider(root *store.Root) (llm.Provider, error) {
	return RoleProvider(root, llm.RolePlanning)
}

// RoleProvider resolves one role through providers.yaml. No providers file is
// not a fault: it means this installation routes nothing.
func RoleProvider(root *store.Root, role llm.Role) (llm.Provider, error) {
	f, err := llm.LoadProvidersFile(root.Layout().ConfigDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load providers.yaml: %w", err)
	}
	// The router's second argument was the offline flag, which no longer
	// exists: a remote decision service is required, so there is no mode in
	// which a remote provider must be refused on principle.
	router, err := llm.NewRouter(f)
	if err != nil {
		return nil, err
	}
	return router.For(role)
}

// ServicePorts lists the ports this installation's own services listen on, so
// a sandbox can deny them even inside the ephemeral range.
func ServicePorts(cfg config.Config) []uint16 {
	var out []uint16
	add := func(p int) {
		if p > 0 && p <= 65535 {
			out = append(out, uint16(p)) //nolint:gosec // bounds checked above
		}
	}
	if _, portStr, err := net.SplitHostPort(cfg.API.Addr); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			add(p)
		}
	}
	add(cfg.Inference.Port)
	add(cfg.Egress.DepsPort)
	add(cfg.Egress.DocsPort)
	return out
}

// Analyzers builds the language and infrastructure analyzers.
func Analyzers(warnf func(string, ...any)) []index.Analyzer {
	goa := golang.New()
	goa.Warnf = warnf
	sqla := sqlan.New()
	sqla.Warnf = warnf
	dep := deploy.New()
	dep.Warnf = warnf
	tf := terraform.New()
	tf.Warnf = warnf
	gitl := gitlog.New()
	gitl.Warnf = warnf
	ts := typescript.New()
	ts.Warnf = warnf
	// The sidecar gives compiler-backed edges when installed; without it the
	// lexical reading runs and its edges say they are weaker.
	ts.UseSidecar = true
	pa := protoavro.New()
	pa.Warnf = warnf
	arch := architecture.New()
	arch.Warnf = warnf
	return []index.Analyzer{goa, ts, sqla, pa, dep, tf, arch, gitl}
}

// Semantic returns the compiler-backed indexer for the languages that have
// one. It is separate from Analyzers because it is not one: it runs once over
// the whole repository, after the file layer exists, and writes through
// internal/scipindex rather than returning nodes and edges.
//
// Nil for a repository it does not apply to, which index.Repository checks
// before launching anything.
func Semantic(logf func(string, ...any)) index.SemanticIndexer {
	return &python.Indexer{Logf: logf}
}

// loadOracle loads the hidden acceptance suite, refusing one that sits inside
// the repository. A task's model can read the repository; a hidden check it can
// read is a visible check that pretends otherwise. An empty dir means none.
func loadOracle(dir, repoRoot string) (*oracle.Suite, error) {
	if dir == "" {
		return nil, nil
	}
	// Location first: whatever an in-repository directory holds, the answer
	// is the same, and a parse error would hide the reason that matters.
	if repoRoot != "" {
		outside, err := oracle.Outside(dir, repoRoot)
		if err != nil {
			return nil, fmt.Errorf("oracle: %w", err)
		}
		if !outside {
			return nil, fmt.Errorf("oracle: %s is inside the repository %s, where the model "+
				"can read it; keep hidden checks outside every repository a task works on", dir, repoRoot)
		}
	}
	return oracle.Load(dir)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
