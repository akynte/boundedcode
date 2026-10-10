package main

import (
	"strings"
	"testing"
)

const stream = `{"Action":"run","Package":"example.com/p","Test":"TestOK"}
{"Action":"output","Package":"example.com/p","Test":"TestOK","Output":"=== RUN   TestOK\n"}
{"Action":"pass","Package":"example.com/p","Test":"TestOK"}
{"Action":"run","Package":"example.com/p","Test":"TestDocker"}
{"Action":"output","Package":"example.com/p","Test":"TestDocker","Output":"=== RUN   TestDocker\n"}
{"Action":"output","Package":"example.com/p","Test":"TestDocker","Output":"    d_test.go:9: set BC_TEST_DOCKER_IMAGE\n"}
{"Action":"output","Package":"example.com/p","Test":"TestDocker","Output":"--- SKIP: TestDocker (0.00s)\n"}
{"Action":"skip","Package":"example.com/p","Test":"TestDocker"}
{"Action":"output","Package":"example.com/p","Test":"TestBad","Output":"    b_test.go:3: boom | pipe\n"}
{"Action":"fail","Package":"example.com/p","Test":"TestBad"}
{"Action":"fail","Package":"example.com/p"}
# example.com/broken
{"Action":"fail","Package":"example.com/broken"}
`

func TestSummarize(t *testing.T) {
	var log strings.Builder
	s, err := summarize(strings.NewReader(stream), &log)
	if err != nil {
		t.Fatal(err)
	}
	if s.passed != 1 || len(s.failed) != 1 || len(s.skipped) != 1 || s.failedPackageCount() != 2 {
		t.Fatalf("passed %d failed %v skipped %v packages %d", s.passed, s.failed, s.skipped, s.failedPackageCount())
	}
	if got := s.skipped[key{"example.com/p", "TestDocker"}]; got != "d_test.go:9: set BC_TEST_DOCKER_IMAGE" {
		t.Fatalf("skip reason %q", got)
	}
	if !strings.Contains(log.String(), "boom | pipe") || !strings.Contains(log.String(), "# example.com/broken") {
		t.Fatalf("test output not passed through:\n%s", log.String())
	}
	md := s.markdown("macOS")
	for _, want := range []string{"### Go tests (macOS)", "| 1 | 1 | 1 | 2 |", "- `example.com/p` TestBad",
		"- `example.com/broken` (package: build failure", "TestDocker: d_test.go:9: set BC_TEST_DOCKER_IMAGE"} {
		if !strings.Contains(md, want) {
			t.Errorf("summary lacks %q:\n%s", want, md)
		}
	}
}
