// Package evidence is the Standard Evidence Suite v1 orchestrator.
//
// It builds the 16 planned runs (8 frozen tasks x 2 arms) from the frozen
// manifest, drives each through the real, unmodified BoundedCode
// pipeline (internal/task.Runner -> internal/engine -> internal/recipe ->
// internal/sandbox), and persists durable, append-only run state so a
// multi-hour live campaign survives interruption without a hidden rerun.
//
// It owns none of task supervision, generator reasoning, or Jev policy —
// those stay inside internal/task.Runner exactly as they are for any other
// task. This package's only job is benchmark/environment adaptation: which
// image, which sanitized workspace, which arm's judgment profile, and where
// the result goes.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Arm names one leg of the ablation. Never called "ON"/"OFF" in a
// persisted record: EXPERIMENTAL_JEV_FULL_AUTHORITY is a materially
// stronger condition than ordinary "Jev on" (every registered site
// promoted to its own ceiling at once, for this experiment only — see
// evals/evidence-v1/jev-profile.yaml), and calling it anything vaguer would
// misstate what is actually being tested.
type Arm string

const (
	ArmControl             Arm = "CONTROL"
	ArmExperimentalJevFull Arm = "EXPERIMENTAL_JEV_FULL_AUTHORITY"
)

// Benchmark names which family a task belongs to.
type Benchmark string

const (
	BenchmarkSWEBenchProVerified Benchmark = "swe_bench_pro_verified"
	BenchmarkSWEAtlasTestWriting Benchmark = "swe_atlas_test_writing"
	BenchmarkSWEAtlasRefactoring Benchmark = "swe_atlas_refactoring"
)

// TaskRegistryEntry is the per-task metadata this package needs beyond the
// bare task ID the frozen manifest carries — the pinned image, the
// benchmark-native runtime shape, and enough provenance to construct a
// sandbox.Spec / eval.TaskOrigin without re-deriving it from the network on
// every run. It is cross-validated against manifest.json's task_ids at
// load time (LoadRegistry), so a typo or an out-of-date entry here is a
// hard error rather than a silent mismatch with the frozen suite.
type TaskRegistryEntry struct {
	TaskID     string    `json:"task_id"`
	Benchmark  Benchmark `json:"benchmark"`
	Repository string    `json:"repository"`
	Language   string    `json:"language,omitempty"`
	// ImageRef is the pinned image reference, digest form
	// (name@sha256:...) — required, never a tag, matching
	// internal/sandbox/container.Pinned's own refusal of one.
	ImageRef string `json:"image_ref"`
	// ImageDir is where the workspace is mounted inside ImageRef.
	ImageDir string `json:"image_dir"`
}

// Registry is the full, frozen-cross-validated task list for Evidence
// Suite v1 — the canonical source this package's task loader reads,
// design instruction §1's "read the eight task definitions from the frozen
// Evidence Suite artifacts... do not hardcode a separate list unless the
// frozen artifact itself is the canonical list." The frozen manifest is the
// canonical list of *which 8 task IDs*; it does not itself carry image
// digests or benchmark-native runtime shape (design instruction §18 notes
// the manifest's own generator_model field is deliberately "UNSET" — the
// manifest fixes selection, not runtime config), so this registry supplies
// exactly that, and LoadRegistry refuses to proceed if its task IDs do not
// match the frozen manifest's exactly.
var Registry = []TaskRegistryEntry{
	{
		TaskID:     "instance_future-architect__vuls-bff6b7552370b55ff76d474860eead4ab5de785a-v1151a6325649aaf997cd541ebe533b53fddf1b07",
		Benchmark:  BenchmarkSWEBenchProVerified,
		Repository: "future-architect/vuls",
		Language:   "go",
		ImageRef:   "jefzda/sweap-images@sha256:e5310e43e886b49210c59ca5c79571235f6c38ccd50035e1bf4bd6bac02a6034",
		ImageDir:   "/app",
	},
	{
		TaskID:     "instance_element-hq__element-web-ca58617cee8aa91c93553449bfdf9b3465a5119b-vnan",
		Benchmark:  BenchmarkSWEBenchProVerified,
		Repository: "element-hq/element-web",
		Language:   "js",
		ImageRef:   "jefzda/sweap-images@sha256:ae3932151d19d364fbafbca8bde0460ffe3ae6f8c6ef8fa4075debc0daae1665",
		ImageDir:   "/app",
	},
	{
		TaskID:     "instance_internetarchive__openlibrary-1894cb48d6e7fb498295a5d3ed0596f6f603b784-v0f5aece3601a5b4419f7ccec1dbda2071be28ee4",
		Benchmark:  BenchmarkSWEBenchProVerified,
		Repository: "internetarchive/openlibrary",
		Language:   "python",
		ImageRef:   "jefzda/sweap-images@sha256:dc5dd99a9a808e6d4e6d4330f5bde7858da382ec9058dda236330d0962ce871b",
		ImageDir:   "/app",
	},
	{
		TaskID:     "instance_tutao__tutanota-b4934a0f3c34d9d7649e944b183137e8fad3e859-vbc0d9ba8f0071fbe982809910959a6ff8884dbbf",
		Benchmark:  BenchmarkSWEBenchProVerified,
		Repository: "tutao/tutanota",
		Language:   "ts",
		ImageRef:   "jefzda/sweap-images@sha256:b94241cfbb3324e3f8970fef92f54b9464e1b7424622c405b2905800daea0f21",
		ImageDir:   "/app",
	},
	{
		TaskID:     "task-6902ef3ab97fe23e2ad271f5",
		Benchmark:  BenchmarkSWEAtlasTestWriting,
		Repository: "Automattic/wp-calypso",
		ImageRef:   "ghcr.io/scaleapi/swe-atlas@sha256:98333a6fe02365449e22ea04bbd172feb5877e54a528f0bddd08aa826c95283e",
		ImageDir:   "/app",
	},
	{
		TaskID:     "task-6902ef3ab97fe23e2ad2727a",
		Benchmark:  BenchmarkSWEAtlasTestWriting,
		Repository: "drakkan/sftpgo",
		ImageRef:   "ghcr.io/scaleapi/swe-atlas@sha256:74abefa63f68d15537c4b14745bd44ba0e3d35477e923baf58bfbe70e59e40b6",
		ImageDir:   "/app",
	},
	{
		TaskID:     "task-694b4b99829f00e24fd11891",
		Benchmark:  BenchmarkSWEAtlasRefactoring,
		Repository: "Automattic/wp-calypso",
		Language:   "JavaScript",
		ImageRef:   "ghcr.io/scaleapi/swe-atlas@sha256:0dd613c90d6bcfc266fb8f266d350d5e6236872425bf86d022c1847b28a5d6d3",
		ImageDir:   "/calypso",
	},
	{
		TaskID:     "task-69b7c2a04b6f8ff9ed98812c",
		Benchmark:  BenchmarkSWEAtlasRefactoring,
		Repository: "MariaDB/server",
		Language:   "C++",
		ImageRef:   "ghcr.io/scaleapi/swe-atlas@sha256:99c95c909422f61eafa3064e0dd8a87314e380ee0587f7cf89fb5dd6bf0f1afb",
		ImageDir:   "/app/source",
	},
}

// ManifestTaskIDs is manifest.json's task_ids block, in exactly the shape
// evals/evidence-v1/manifest.json's "task_ids" object has it.
type ManifestTaskIDs struct {
	SWEBenchProVerified []string `json:"swe_bench_pro_verified"`
	SWEAtlasTestWriting []string `json:"swe_atlas_test_writing"`
	SWEAtlasRefactoring []string `json:"swe_atlas_refactoring"`
}

// Manifest is the fields of the frozen manifest.json this package reads.
// Unknown fields are ignored deliberately — this package must never fail
// to load a legitimately frozen manifest merely because it grew a field
// this reader does not yet know about.
type Manifest struct {
	TaskIDs       ManifestTaskIDs `json:"task_ids"`
	JevProfileSHA string          `json:"jev_profile_sha256"`
	SelectionSHA  string          `json:"selection_sha256"`
}

// LoadManifest reads and parses the frozen manifest.
func LoadManifest(path string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("evidence: parsing manifest: %w", err)
	}
	return m, nil
}

// allManifestTaskIDs returns every task ID the manifest names, in the
// manifest's own declared order (SWE-Bench Pro Verified, then TW, then RF)
// — the frozen run order design instruction §9 requires within a benchmark
// family, and the order §8/§48's plan construction must reproduce exactly.
func (m Manifest) allTaskIDs() []string {
	var out []string
	out = append(out, m.TaskIDs.SWEBenchProVerified...)
	out = append(out, m.TaskIDs.SWEAtlasTestWriting...)
	out = append(out, m.TaskIDs.SWEAtlasRefactoring...)
	return out
}

// LoadRegistry cross-validates Registry against the frozen manifest and
// returns the registry entries in the manifest's own order. It is an error
// if the two do not name exactly the same 8 task IDs — this is the
// guardrail design instruction §1 asks for ("do not hardcode a separate
// list"): Registry supplies data the manifest deliberately does not carry,
// but it may never silently diverge from what the manifest actually
// selected.
func LoadRegistry(manifestPath string) ([]TaskRegistryEntry, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]TaskRegistryEntry, len(Registry))
	for _, e := range Registry {
		byID[e.TaskID] = e
	}

	ids := m.allTaskIDs()
	if len(ids) != len(Registry) {
		return nil, fmt.Errorf("evidence: manifest names %d task(s), registry has %d — "+
			"they must match exactly", len(ids), len(Registry))
	}
	out := make([]TaskRegistryEntry, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		e, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("evidence: manifest names task %q, which is not in the "+
				"registry — the registry is stale relative to the frozen manifest", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("evidence: manifest names task %q more than once", id)
		}
		seen[id] = true
		out = append(out, e)
	}
	return out, nil
}

// RunPlan is one planned run: one task, one arm. BuildPlan produces exactly
// 16 of these — design instruction §8 — before any is executed.
type RunPlan struct {
	SuiteHash        string    `json:"suite_hash"`
	RunOrder         int       `json:"run_order"`
	TaskID           string    `json:"task_id"`
	Benchmark        Benchmark `json:"benchmark"`
	Repository       string    `json:"repository"`
	Language         string    `json:"language,omitempty"`
	Arm              Arm       `json:"arm"`
	ImageRef         string    `json:"image_ref"`
	ImageDir         string    `json:"image_dir"`
	JevProfileSHA    string    `json:"jev_profile_sha256,omitempty"` // set only for the treatment arm
	BudgetRunsPerArm int       `json:"budget_runs_per_arm"`
	NetworkPolicy    string    `json:"network_policy"`
	// Populated by Prepare, not BuildPlan: the sanitized workspace path and
	// its content fingerprint. Empty in a plan that has not been prepared.
	WorkspacePath     string `json:"workspace_path,omitempty"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
}

// key identifies one (task, arm) pair for ledger lookups.
func (p RunPlan) Key() string { return p.TaskID + "|" + string(p.Arm) }

// BuildPlan constructs all 16 planned runs from the frozen manifest and
// the cross-validated registry: every SWE-Bench Pro Verified, then every
// SWE Atlas Test Writing, then every SWE Atlas Refactoring task, each run
// under CONTROL then under EXPERIMENTAL_JEV_FULL_AUTHORITY — design
// instruction §9's frozen run order ("all 8 CONTROL runs, then all 8
// EXPERIMENTAL_JEV_FULL_AUTHORITY runs") is expressed by RunOrder, which a
// caller sorts on before executing rather than this function interleaving
// arms itself; see Plan.Ordered.
func BuildPlan(manifestPath, jevProfilePath string) ([]RunPlan, error) {
	entries, err := LoadRegistry(manifestPath)
	if err != nil {
		return nil, err
	}
	suiteHash, err := sha256File(manifestPath)
	if err != nil {
		return nil, err
	}
	profileHash, err := sha256File(jevProfilePath)
	if err != nil {
		return nil, err
	}

	var plans []RunPlan
	order := 0
	for _, arm := range []Arm{ArmControl, ArmExperimentalJevFull} {
		for _, e := range entries {
			order++
			p := RunPlan{
				SuiteHash:        suiteHash,
				RunOrder:         order,
				TaskID:           e.TaskID,
				Benchmark:        e.Benchmark,
				Repository:       e.Repository,
				Language:         e.Language,
				Arm:              arm,
				ImageRef:         e.ImageRef,
				ImageDir:         e.ImageDir,
				BudgetRunsPerArm: 1,
				NetworkPolicy:    string(networkPolicyFor(e.Benchmark)),
			}
			if arm == ArmExperimentalJevFull {
				p.JevProfileSHA = profileHash
			}
			plans = append(plans, p)
		}
	}
	if len(plans) != 16 {
		return nil, fmt.Errorf("evidence: built %d run(s), expected 16", len(plans))
	}
	return plans, nil
}

type networkPolicy string

func networkPolicyFor(b Benchmark) networkPolicy {
	if b == BenchmarkSWEBenchProVerified {
		return "agentcompass_anti_leakage_blocklist"
	}
	return "harbor_allowlist"
}

func sha256File(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Ordered sorts plans by RunOrder — the frozen sequence a live campaign
// must actually follow (design instruction §9): every CONTROL run in
// frozen task order, then every EXPERIMENTAL_JEV_FULL_AUTHORITY run in the
// same task order.
func Ordered(plans []RunPlan) []RunPlan {
	out := append([]RunPlan(nil), plans...)
	sort.Slice(out, func(i, j int) bool { return out[i].RunOrder < out[j].RunOrder })
	return out
}

// CountByBenchmark reports how many of plans belong to each benchmark
// family, for a "N CONTROL run configs ready" style report (design
// instruction §35).
func CountByArmAndBenchmark(plans []RunPlan) map[Arm]map[Benchmark]int {
	out := map[Arm]map[Benchmark]int{}
	for _, p := range plans {
		if out[p.Arm] == nil {
			out[p.Arm] = map[Benchmark]int{}
		}
		out[p.Arm][p.Benchmark]++
	}
	return out
}
