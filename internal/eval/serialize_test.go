package eval

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// nonFinite walks a value and names every float that JSON cannot carry.
//
// A targeted test would only catch the field somebody thought of. The report
// is a wide struct of derived statistics, and every one of them is a division
// whose denominator can be zero on a run that measured nothing — which is
// exactly the run a benchmark produces when it is going badly and the report
// matters most.
func nonFinite(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Float64, reflect.Float32:
		if f := v.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			t.Errorf("%s = %v, which encoding/json cannot represent", path, f)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" {
				continue // unexported
			}
			nonFinite(t, v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			nonFinite(t, v.Index(i), path+"[]")
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			nonFinite(t, v.MapIndex(k), path+"["+k.String()+"]")
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			nonFinite(t, v.Elem(), path+"*")
		}
	}
}

// reportShapes are the degenerate runs a report has to survive. Each one was
// producible by `bcode eval run` and each one is a denominator that can be zero.
func reportShapes() map[string]struct {
	outcomes []Outcome
	tasks    []Task
} {
	task := Task{ID: "T1", Category: "bug_fix", LeakRisk: "public", Set: "dev"}
	base := Outcome{
		RunID: "r1", TaskID: "T1", Arm: "supervised", Status: StatusTaskFailed,
		Category: "bug_fix", LeakRisk: "public", Set: "dev", Attempts: 1,
	}
	errored := base
	errored.Status, errored.Err = StatusEnvironmentFailed, "baseline environment"

	graded := base
	graded.Grade = Grade{Total: 4, Passed: 1}

	type shape = struct {
		outcomes []Outcome
		tasks    []Task
	}
	return map[string]shape{
		// The shape this session actually hit: one arm, nothing graded.
		"nothing graded":     {[]Outcome{base}, []Task{task}},
		"no outcomes at all": {nil, []Task{task}},
		"no tasks at all":    {nil, nil},
		"every run errored":  {[]Outcome{errored}, []Task{task}},
		"one graded run":     {[]Outcome{graded}, []Task{task}},
		"graded and not":     {[]Outcome{graded, base}, []Task{task}},
	}
}

// A report must always serialize. A metric that could not be measured is
// reported as absent, never as zero, and never as a value that fails the
// encoder and takes the whole run's record with it.
func TestReportAlwaysSerializes(t *testing.T) {
	for name, s := range reportShapes() {
		t.Run(name, func(t *testing.T) {
			rep := Aggregate(s.outcomes, s.tasks)
			nonFinite(t, reflect.ValueOf(rep), "Report")
			body, err := json.Marshal(rep)
			if err != nil {
				t.Fatalf("the report does not serialize: %v", err)
			}
			var back map[string]any
			if err := json.Unmarshal(body, &back); err != nil {
				t.Fatalf("the report does not round-trip: %v", err)
			}
			// Formatting must survive the same shapes: it is what a person
			// reads when the JSON is the thing that failed.
			_ = rep.Format()
		})
	}
}

// An unmeasured mean is null, not zero. Zero is the score of a run that was
// graded and passed nothing, and a reader cannot tell the two apart.
func TestUnmeasuredMeanScoreIsNullNotZero(t *testing.T) {
	rep := Aggregate([]Outcome{{
		RunID: "r1", TaskID: "T1", Arm: "supervised", Status: StatusTaskFailed,
		Category: "bug_fix", LeakRisk: "public", Set: "dev", Attempts: 1,
	}}, []Task{{ID: "T1", Category: "bug_fix", LeakRisk: "public", Set: "dev"}})

	if len(rep.Arms) != 1 {
		t.Fatalf("got %d arms, want 1", len(rep.Arms))
	}
	if rep.Arms[0].GradedRuns != 0 {
		t.Fatalf("graded runs = %d, want 0", rep.Arms[0].GradedRuns)
	}
	if rep.Arms[0].MeanScore != nil {
		t.Errorf("mean score = %v, want nil for a run nothing graded", *rep.Arms[0].MeanScore)
	}

	body, err := json.Marshal(rep.Arms[0])
	if err != nil {
		t.Fatalf("arm does not serialize: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	v, present := back["mean_score"]
	if !present {
		t.Fatal("mean_score is missing; the schema keeps the key and nulls it")
	}
	if v != nil {
		t.Errorf("mean_score = %v, want null", v)
	}
}
