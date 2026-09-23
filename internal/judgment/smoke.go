package judgment

import (
	"context"
	"fmt"
)

// Smoke is one minimal live call, used to confirm that the configured model
// identifier is one the service actually accepts.
//
// This exists because a benchmark cannot be trusted to a model id nobody has
// checked. The documented identifier for this integration was, at one point,
// a version string that had never been sent anywhere — it looked plausible,
// every unit test used it against an httptest server, and nothing in the
// repository could have told the difference between a real snapshot and an
// invented one.
//
// It is deliberately not a unit test. Unit tests must not need the network or
// a key; `bcode judgment smoke` is the explicit action an operator takes before
// depending on a configuration.
type SmokeResult struct {
	// Requested is the model id from judgment.yaml; Served is what the
	// service reported running, when it reports one. A difference between
	// them is the thing worth seeing.
	Requested string
	Served    string
	// Answered is the probability that came back, as evidence the round trip
	// produced a usable judgment rather than an empty envelope.
	Answered bool
	Noul     float64
	Usage    Usage
}

// Smoke asks one trivial question with no repository content of any kind.
func Smoke(ctx context.Context, j Judge) (SmokeResult, error) {
	c, ok := j.(*Client)
	if !ok {
		return SmokeResult{}, fmt.Errorf("judgment: no live judge is configured; " +
			"set enabled in judgment.yaml and export the key")
	}
	// The credential check is explicit rather than folded into Available() so
	// that the operator is told which of the two reasons applies. A smoke test
	// exists to make one real request, so it has no unauthenticated mode to
	// fall back to.
	if err := c.cfg.RequireCredential(); err != nil {
		return SmokeResult{}, err
	}
	if !c.Available() {
		return SmokeResult{}, fmt.Errorf("judgment: no live judge is configured; " +
			"set enabled in judgment.yaml and export the key")
	}
	res := SmokeResult{Requested: c.cfg.Model}

	st := NewState(RedactStrict)
	if err := st.Objective("confirm that this model identifier is accepted"); err != nil {
		return res, err
	}
	if err := st.TrustedFact("check", "smoke"); err != nil {
		return res, err
	}

	answers, note := AskAll(ctx, j, st, map[string]Question{
		"ok": Noul("Is `objective` a request to verify that a service is reachable?"),
	})
	if !note.Used() {
		return res, fmt.Errorf("judgment: %s", note.Detail)
	}
	a := answers["ok"]
	res.Answered, res.Noul, res.Usage = a.Answered, a.Noul, note.Usage
	res.Served = c.servedModel()
	if !res.Answered {
		return res, fmt.Errorf("judgment: %s accepted the request but answered nothing; "+
			"the model id may not be one it serves", c.cfg.Endpoint)
	}
	return res, nil
}

// servedModel reports the model the last response named.
func (c *Client) servedModel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastModel
}
