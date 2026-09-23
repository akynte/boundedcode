package evidence

import "fmt"

// ArmDiff is the machine-readable comparison design instruction §17/§37
// asks for: two run plans for the same task, one per arm, and every field
// that differs between them.
type ArmDiff struct {
	TaskID         string   `json:"task_id"`
	OnlyJevDiffers bool     `json:"only_jev_differs"`
	Differences    []string `json:"differences,omitempty"`
}

// approvedDifference is the one field family this comparison must find
// different: the Jev profile hash (present only on the treatment run) and
// the arm label itself. Everything else must match.
func approvedDifference(field string) bool {
	switch field {
	case "arm", "jev_profile_sha256":
		return true
	default:
		return false
	}
}

// CompareArms compares a task's CONTROL and treatment RunPlan and reports
// every field that differs. It fails (OnlyJevDiffers=false) if anything
// outside the approved Jev-related fields differs — image, workspace base,
// budget, network policy, or anything else design instruction §37 lists.
func CompareArms(control, treatment RunPlan) ArmDiff {
	d := ArmDiff{TaskID: control.TaskID, OnlyJevDiffers: true}
	if control.TaskID != treatment.TaskID {
		d.TaskID = control.TaskID + " vs " + treatment.TaskID
		d.OnlyJevDiffers = false
		d.Differences = append(d.Differences, "task_id")
		return d
	}

	check := func(field string, a, b any) {
		if fmt.Sprint(a) != fmt.Sprint(b) {
			if !approvedDifference(field) {
				d.OnlyJevDiffers = false
			}
			d.Differences = append(d.Differences, field)
		}
	}
	check("arm", control.Arm, treatment.Arm)
	check("benchmark", control.Benchmark, treatment.Benchmark)
	check("repository", control.Repository, treatment.Repository)
	check("language", control.Language, treatment.Language)
	check("image_ref", control.ImageRef, treatment.ImageRef)
	check("image_dir", control.ImageDir, treatment.ImageDir)
	check("budget_runs_per_arm", control.BudgetRunsPerArm, treatment.BudgetRunsPerArm)
	check("network_policy", control.NetworkPolicy, treatment.NetworkPolicy)
	check("jev_profile_sha256", control.JevProfileSHA, treatment.JevProfileSHA)
	// Source fingerprint is checked only when both are populated (i.e.
	// after Prepare has run for both arms); an unprepared plan pair
	// legitimately has two empty fingerprints, which trivially match and
	// is not a finding either way.
	if control.SourceFingerprint != "" || treatment.SourceFingerprint != "" {
		check("source_fingerprint", control.SourceFingerprint, treatment.SourceFingerprint)
	}
	return d
}

// CompareAllArms runs CompareArms for every task in plans (which must
// contain exactly one CONTROL and one treatment RunPlan per task) and
// returns one ArmDiff per task, in frozen task order.
func CompareAllArms(plans []RunPlan) ([]ArmDiff, error) {
	byTask := map[string]struct{ control, treatment *RunPlan }{}
	order := []string{}
	for i := range plans {
		p := &plans[i]
		entry := byTask[p.TaskID]
		switch p.Arm {
		case ArmControl:
			entry.control = p
		case ArmExperimentalJevFull:
			entry.treatment = p
		default:
			return nil, fmt.Errorf("evidence: unknown arm %q for task %s", p.Arm, p.TaskID)
		}
		if _, seen := byTask[p.TaskID]; !seen {
			order = append(order, p.TaskID)
		}
		byTask[p.TaskID] = entry
	}
	var out []ArmDiff
	for _, id := range order {
		e := byTask[id]
		if e.control == nil || e.treatment == nil {
			return nil, fmt.Errorf("evidence: task %s is missing one arm's plan", id)
		}
		out = append(out, CompareArms(*e.control, *e.treatment))
	}
	return out, nil
}
