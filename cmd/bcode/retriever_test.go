package main

import (
	"context"
	"testing"

	"github.com/akynte/boundedcode/internal/memory"
	"github.com/akynte/boundedcode/internal/retrieval"
	"github.com/akynte/boundedcode/internal/store"
	"github.com/akynte/boundedcode/internal/supervisor"
	"github.com/akynte/boundedcode/internal/workspace"
)

// The wiring step is the one that keeps going wrong in this repository: a
// value computed correctly, a consumer that reads it, and nothing joining
// them. This asserts the join, because both halves already have their own
// tests and both passed while the feature did nothing.
func TestTheRetrieverGetsTheRepositorysNotes(t *testing.T) {
	repo := t.TempDir()
	m := memory.Open(repo, memory.DefaultCaps())
	if _, err := m.Add(memory.Note{
		Kind: memory.KindAdvice, Text: "WIRED-MARKER",
		Provenance: memory.Provenance{Source: "operator"},
	}); err != nil {
		t.Fatalf("add note: %v", err)
	}

	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(context.Background(),
		workspace.DeriveID(repo, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}

	r := retrieverFor(st, &workspace.Workspace{Root: repo}, root, nil)
	pkt, err := r.Build(context.Background(), retrieval.Request{Query: "anything"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(pkt.Notes) == 0 {
		t.Fatal("the retriever the CLI builds carries no notes; the memory store is not attached")
	}
	if pkt.Notes[0].Text != "WIRED-MARKER" {
		t.Errorf("wrong note: %q", pkt.Notes[0].Text)
	}
}

// Not every caller has a checkout — `bcode eval` runs against scratch copies —
// and a missing workspace must degrade to no notes rather than panic.
func TestNoWorkspaceMeansNoNotes(t *testing.T) {
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(context.Background(),
		workspace.DeriveID("/x", "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}

	pkt, err := retrieverFor(st, nil, root, nil).Build(context.Background(), retrieval.Request{Query: "x"})
	if err != nil {
		t.Fatalf("a retriever with no workspace must still build: %v", err)
	}
	if len(pkt.Notes) != 0 {
		t.Error("notes appeared without a workspace to read them from")
	}
}

// The retriever the CLI hands the native engine must carry the judge.
//
// It did not, and nothing said so: every M8 consultation on the EDIT path
// reported no_judge while the Runner's own retriever reported a live judge
// for the same task. context_injection exists to screen what reaches the
// generator's context, and the generator's retrieval is this retriever — so
// an unjudged one turns that site off exactly where it is meant to work.
func TestTheEngineRetrieverCarriesTheJudge(t *testing.T) {
	repo := t.TempDir()
	root, err := store.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.CloseAll()
	st, err := root.OpenWorkspace(context.Background(),
		workspace.DeriveID(repo, "", t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	r := retrieverFor(st, &workspace.Workspace{Root: repo}, root, nil)
	if !r.HasJudge() {
		t.Fatal("the engine's retriever has no judge at all; M8 sites skip with no_judge")
	}
	// Parity is the real invariant, and it is what was broken: whatever the
	// supervisor gives the Runner's retriever, this one must have too. An
	// unconfigured workspace yields judgment.Off() on both sides, so this
	// asserts they agree rather than that a live judge exists.
	want, err := supervisor.Judge(root, st, nil)
	if err != nil {
		t.Fatalf("supervisor judge: %v", err)
	}
	if got := r.JudgeAvailable(); got != want.Available() {
		t.Errorf("engine retriever judge Available()=%v, supervisor's =%v; "+
			"the two retrievers disagree about whether a judge exists", got, want.Available())
	}
}
