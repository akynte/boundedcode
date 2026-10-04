package xservice

import "sort"

// LinkOptions control linking.
type LinkOptions struct {
	// SameRepoHTTP links routes and calls within one repository (off by
	// default: in-process calls are rarely contracts).
	SameRepoHTTP bool
}

// LinkAll joins endpoints from all repositories into contract links:
//   - http:  client call -> route (method-compatible, path template match),
//     across repositories;
//   - topic: producer -> consumer of the same topic, across repositories;
//   - topic_infra: provisioner (e.g. Terraform) -> producer/consumer;
//   - env:   reader -> provider (same or different repository).
func LinkAll(eps []Endpoint, opt LinkOptions) []Link {
	by := map[Kind][]Endpoint{}
	for _, e := range eps {
		by[e.Kind] = append(by[e.Kind], e)
	}
	var out []Link
	for _, c := range by[HTTPCall] {
		for _, r := range by[HTTPRoute] {
			if (c.Repo == r.Repo && !opt.SameRepoHTTP) || !pathsMatch(r.Path, c.Path) {
				continue
			}
			if r.Method != "" && c.Method != "" && r.Method != c.Method {
				continue
			}
			out = append(out, Link{Kind: "http", Contract: r.Key(), From: c, To: r})
		}
	}
	for _, p := range by[TopicProduce] {
		for _, c := range by[TopicConsume] {
			if p.Topic == c.Topic && p.Repo != c.Repo {
				out = append(out, Link{Kind: "topic", Contract: "topic " + p.Topic, From: p, To: c})
			}
		}
	}
	for _, pr := range by[TopicProvision] {
		for _, k := range []Kind{TopicProduce, TopicConsume} {
			for _, u := range by[k] {
				if pr.Topic == u.Topic {
					out = append(out, Link{Kind: "topic_infra", Contract: "topic " + pr.Topic, From: pr, To: u})
				}
			}
		}
	}
	for _, rd := range by[EnvRead] {
		for _, pv := range by[EnvProvide] {
			if rd.Env == pv.Env {
				out = append(out, Link{Kind: "env", Contract: "env " + rd.Env, From: rd, To: pv})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Contract != b.Contract {
			return a.Contract < b.Contract
		}
		if a.From.Where() != b.From.Where() {
			return a.From.Where() < b.From.Where()
		}
		return a.To.Where() < b.To.Where()
	})
	return dedupe(out)
}

func dedupe(ls []Link) []Link {
	seen := map[string]bool{}
	var out []Link
	for _, l := range ls {
		k := l.Kind + "|" + l.From.Where() + "|" + l.To.Where() + "|" + l.Contract
		if !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}

// Unresolved reports endpoints whose contract value could not be determined
// well enough to link (e.g. an HTTP call whose path is entirely dynamic).
func Unresolved(eps []Endpoint) []Endpoint {
	var out []Endpoint
	for _, e := range eps {
		switch e.Kind {
		case HTTPCall, HTTPRoute:
			if e.Path == "" {
				out = append(out, e)
			}
		case TopicProduce, TopicConsume, TopicProvision:
			if e.Topic == "" {
				out = append(out, e)
			}
		}
	}
	return out
}
