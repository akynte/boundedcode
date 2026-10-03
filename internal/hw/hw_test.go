package hw

import "testing"

func TestParseGPUs(t *testing.T) {
	out := []byte("0, NVIDIA GeForce RTX 4060 Laptop GPU, 8.9, 615.71.09, 8188, 90, 1, 45, 3.12, 115.00, 210, 0x0000000000000001\n")
	gpus, err := parseGPUs(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d gpus", len(gpus))
	}
	g := gpus[0]
	if g.MemTotalMiB != 8188 || g.ComputeCap != "8.9" || g.PowerLimitW != 115 || g.TempC != 45 {
		t.Fatalf("bad parse: %+v", g)
	}
}

func TestProbeDoesNotFail(t *testing.T) {
	s := Probe(t.Context())
	if s.LogicalCPUs == 0 {
		t.Fatal("no cpus")
	}
}
