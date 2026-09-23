package supervisor

import (
	"context"

	"github.com/akynte/boundedcode/internal/cache"
	"github.com/akynte/boundedcode/internal/judgment"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
)

// Wiring for internal/judgment.
//
// The judgment package declares what it needs — a Recorder and an AnswerCache
// — as interfaces, and does not import the store. The adapters are here
// because this is where a workspace is open and where everything else is
// assembled, and because §2.3 keeps file I/O behind internal/store.

// Judge builds the judge for a workspace.
//
// Three outcomes, and only one of them is an error: no configuration or
// enabled false yields judgment.Off(), which answers nothing and is the
// ordinary state; a usable configuration yields a live client; and judgments
// a missing credential is a configuration error, because six registered sites
// cannot be decided without one.
func Judge(root *store.Root, st *store.Store, logf func(string, ...any)) (judgment.Judge, error) {
	jcfg, err := judgment.Load(root.Layout().ConfigDir())
	if err != nil {
		return nil, err
	}
	deps := judgment.Deps{Logf: logf}
	if st != nil {
		deps.Recorder = &journal{l: ledger.New(st)}
		if jcfg.Cache {
			deps.Cache = &answers{c: cache.New(st)}
		}
	}
	j, err := judgment.New(jcfg, deps)
	if err != nil {
		return nil, err
	}
	if j.Available() && logf != nil {
		logf("judgment: %s, redact %s", j.Name(), jcfg.Redact)
	}
	return j, nil
}

// journal records judgment requests in the ledger.
//
// The intent is written before the request leaves and the outcome after, which
// is §7.1's contract and is not a formality here: a request that timed out may
// still have been served and billed, and an uncertain row is the honest record
// of not knowing.
type journal struct{ l *ledger.Ledger }

func (j *journal) BeginJudgment(ctx context.Context, in judgment.JournalIntent) (func(judgment.JournalOutcome), error) {
	// A judgment made outside a task has nothing to attach to. Recording it
	// under an empty task id would put it in every task's trace, so it is not
	// recorded at all and the caller is none the wiser: the request is
	// advisory and so is the record of it.
	taskID := judgment.TaskIDFrom(ctx)
	if taskID == "" {
		return func(judgment.JournalOutcome) {}, nil
	}
	h, err := j.l.Begin(ctx, taskID, ledger.KindJudgment, in, "")
	if err != nil {
		return nil, err
	}
	return func(out judgment.JournalOutcome) {
		if out.Err != "" {
			// Interrupted, not Fail: the request may have been served. What is
			// recorded is that this side does not know how it went.
			_ = h.Interrupted(ctx, errString(out.Err))
			return
		}
		_ = h.Complete(ctx, out, "", "")
	}, nil
}

// observer records one row per site consultation, including the clean ones.
//
// It mirrors journal's shape and exists for the same reason: internal/ledger
// cannot import internal/judgment without closing a cycle through worktree, so
// the two types are converted here. Unlike journal it records consultations
// made outside a task as well — a site reached with no task id is still a
// consultation, and its denominator is still real.
type observer struct{ s *ledger.ConsultationStore }

func (o *observer) Consultation(ctx context.Context, c judgment.Consultation) {
	o.s.Consultation(ctx, ledger.Consultation{
		ID: c.ID, TaskID: c.TaskID, Site: c.Site, Phase: c.Phase,
		Reached: c.Reached, Requested: c.Requested,
		Status: string(c.Status), SkipReason: c.SkipReason,
		Questions: c.Questions, Subjects: c.Subjects, Findings: c.Findings,
		Model: c.Model, Endpoint: c.Endpoint, Latency: c.Latency,
		PredictionIDs: c.PredictionIDs, At: c.At,
	})
}

// answers is the content-addressed cache for judgment responses.
//
// It goes through internal/cache rather than store.BlobDir directly, because
// that is where key derivation lives and the storescope analyzer says so.
type answers struct{ c *cache.Cache }

func (a *answers) GetJudgment(key string) ([]byte, bool) {
	body, err := a.c.Get("judgment", key)
	if err != nil {
		return nil, false
	}
	return body, true
}

func (a *answers) PutJudgment(key string, body []byte) {
	// A cache write that fails is a cache miss next time, which is the same
	// outcome as not having written it. Nothing depends on it.
	_ = a.c.Put("judgment", key, body)
}

// SiteAuthority resolves a judgment site to what a replay needs to know
// about it: the question version this build implements, the floor its caller
// applies, and whether the tier configured in cfg lets that caller act.
//
// It lives here for the same reason journal and answers do: internal/ledger
// declares the shape and does not import internal/judgment (that import is a
// cycle — judgment reaches firewall reaches worktree reaches ledger), and
// this package is where a workspace's configuration and its store are both
// in scope.
func SiteAuthority(cfg judgment.Config) ledger.SiteAuthority { return siteAuthority{cfg: cfg} }

type siteAuthority struct{ cfg judgment.Config }

func (s siteAuthority) Lookup(site string) (ledger.ReplaySiteInfo, bool) {
	info, ok := judgment.Site(site)
	if !ok {
		return ledger.ReplaySiteInfo{}, false
	}
	tier := s.cfg.Tier(site)
	// A site whose declared ceiling is Logged implements no effect at any
	// configured tier, so permission is the conjunction of both: what
	// configuration grants and what the site's own code can use.
	permitted := tier.Permits(judgment.TierOrdering) && info.MaxEffect.Permits(judgment.TierOrdering)
	return ledger.ReplaySiteInfo{
		Version:         info.Version,
		EffectThreshold: info.EffectThreshold,
		EffectPermitted: permitted,
		ConfiguredTier:  string(tier),
	}, true
}

// errString turns a recorded message back into an error for the ledger, which
// takes a cause rather than a string.
type errString string

func (e errString) Error() string { return string(e) }

// compile-time proof that the adapters satisfy what judgment asked for.
var (
	_ judgment.Recorder    = (*journal)(nil)
	_ judgment.AnswerCache = (*answers)(nil)
)
