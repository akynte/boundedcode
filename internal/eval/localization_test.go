package eval

import "testing"

// A task with no ground truth is reported as unscorable rather than as a zero.
// The difference matters: a reader who sees 0% recall concludes retrieval
// failed, and what actually happened is that nobody wrote down what the right
// answer was.
func TestUnscorableWithoutGroundTruth(t *testing.T) {
	got := ScoreLocalization(Expected{}, []string{"a.go"}, []string{"a.go"})
	if got == nil {
		t.Fatal("no score was produced at all; a reader cannot count what is missing")
	}
	if got.Status != LocalizationUnscorable {
		t.Fatalf("status = %q, want %q", got.Status, LocalizationUnscorable)
	}
	if got.Scored() {
		t.Fatal("an unscorable run reported itself scored")
	}
	// Symbols alone make Expected "known" but cannot score file recall.
	if s := ScoreLocalization(Expected{Symbols: []string{"X"}}, nil, []string{"a.go"}); s.Scored() {
		t.Fatal("symbols-only ground truth produced a file score")
	}
}

// The measurement the whole split exists for: a file the generator never
// proposed is retrieval's failure; a file it proposed and the packet dropped
// is the ranking's.
func TestGeneratorRecallIsSeparateFromRerankerRecall(t *testing.T) {
	score := ScoreLocalization(
		Expected{Files: []string{"a.go", "b.go", "c.go"}},
		// The generator found a and b, never c.
		[]string{"a.go", "b.go", "z.go"},
		// The packet kept only a: b was proposed and lost in ranking.
		[]string{"a.go"},
	)
	if !score.Scored() {
		t.Fatal("the run was not scored")
	}
	if score.GeneratorRecall < 0.66 || score.GeneratorRecall > 0.67 {
		t.Fatalf("generator recall = %v, want 2/3", score.GeneratorRecall)
	}
	if score.RerankerRecall < 0.33 || score.RerankerRecall > 0.34 {
		t.Fatalf("reranker recall = %v, want 1/3", score.RerankerRecall)
	}
	if len(score.GeneratorMissed) != 1 || score.GeneratorMissed[0] != "c.go" {
		t.Fatalf("generator missed = %v, want [c.go]", score.GeneratorMissed)
	}
	if len(score.LostInRanking) != 1 || score.LostInRanking[0] != "b.go" {
		t.Fatalf("lost in ranking = %v, want [b.go]; c.go was never proposed and is not "+
			"the ranking's failure", score.LostInRanking)
	}
	if score.Precision != 1 {
		t.Fatalf("precision = %v; one expected file out of one carried", score.Precision)
	}
}

// The expectation is written by hand in a task file and the retrieved paths
// come out of an index. A score that treated "./x.go" and "x.go" as different
// files would be measuring the task author's typing.
func TestLocalizationNormalisesPaths(t *testing.T) {
	score := ScoreLocalization(Expected{Files: []string{"./pkg/x.go"}},
		[]string{"pkg/x.go"}, []string{"pkg/./x.go"})
	if score.Recall != 1 || score.GeneratorRecall != 1 {
		t.Fatalf("recall = %v / generator = %v; the same file was counted as two",
			score.Recall, score.GeneratorRecall)
	}
}

// The three-level experiment: a deterministic baseline, a local semantic
// reranker, and the external judge. Without the middle arm a win for the
// judged arm says only that semantic reranking helps, which is not the
// question the egress has to answer.
func TestThreeRerankLevelsExistAndDifferByOneToggle(t *testing.T) {
	base, err := ArmByName("supervised")
	if err != nil {
		t.Fatal(err)
	}
	local, err := ArmByName("supervised-rerank-local")
	if err != nil {
		t.Fatal(err)
	}
	judged, err := ArmByName("supervised-rerank")
	if err != nil {
		t.Fatal(err)
	}
	if base.Rerank != RerankNone || local.Rerank != RerankLocal || judged.Rerank != RerankJudged {
		t.Fatalf("rerank kinds = %q / %q / %q", base.Rerank, local.Rerank, judged.Rerank)
	}
	for _, pair := range [][2]Arm{{base, local}, {local, judged}} {
		a, b := pair[0], pair[1]
		if a.Graph != b.Graph || a.Verification != b.Verification ||
			a.Supervised != b.Supervised || a.Role != b.Role {
			t.Fatalf("%s and %s differ by more than the reranker", a.Name, b.Name)
		}
	}
}

// The comparison that speaks to the egress must be stated in the arm set, not
// left for a reader to assemble.
func TestTheEgressComparisonIsDeclared(t *testing.T) {
	var found bool
	for _, c := range Comparisons() {
		if c.Baseline == "supervised-rerank-local" && c.Variant == "supervised-rerank" {
			found = true
		}
	}
	if !found {
		t.Fatal("no comparison isolates the external judge against the local reranker, " +
			"so nothing in the report answers whether the egress bought anything")
	}
}
