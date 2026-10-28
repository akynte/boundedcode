package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func integrityOf(t *testing.T, base, change map[string]string, remove ...string) []IntegrityFinding {
	t.Helper()
	repo, sha := commitRepo(t, base)
	writeFiles(t, repo, change)
	for _, f := range remove {
		if err := os.Remove(filepath.Join(repo, f)); err != nil {
			t.Fatal(err)
		}
	}
	var changed []string
	for f := range change {
		changed = append(changed, f)
	}
	changed = append(changed, remove...)
	return (&Engine{}).Integrity(context.Background(), RepoTarget{Worktree: repo, Base: sha}, changed)
}

func severities(fs []IntegrityFinding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Severity+":"+f.File)
	}
	return strings.Join(out, " ")
}

// Ways a change can make its own verification pass without testing it, in
// several ecosystems, and changes to the same files that are legitimate.
func TestIntegrity(t *testing.T) {
	pkg := `{"name":"x","scripts":{"test":"node --test","lint":"eslint ."},"dependencies":{"a":"1"}}`
	for name, c := range map[string]struct {
		base, change map[string]string
		remove       []string
		want         string
	}{
		"npm test script": {map[string]string{"package.json": pkg}, map[string]string{"package.json": `{"name":"x","scripts":{"test":"exit 0","lint":"eslint ."},"dependencies":{"a":"1"}}`}, nil, "block:package.json"},
		"npm dependency":  {map[string]string{"package.json": pkg}, map[string]string{"package.json": `{"name":"x","scripts":{"test":"node --test","lint":"eslint --fix ."},"dependencies":{"a":"2","b":"1"}}`}, nil, ""},
		"jest config":     {map[string]string{"jest.config.js": "module.exports={}"}, map[string]string{"jest.config.js": "module.exports={testPathIgnorePatterns:['.']}"}, nil, "review:jest.config.js"},
		"makefile test":   {map[string]string{"Makefile": "build:\n\tgo build ./...\ntest:\n\tgo test ./...\n"}, map[string]string{"Makefile": "build:\n\tgo build ./...\ntest:\n\ttrue\n"}, nil, "block:Makefile"},
		"makefile build":  {map[string]string{"Makefile": "build:\n\tgo build ./...\ntest:\n\tgo test ./...\n"}, map[string]string{"Makefile": "build:\n\tgo build -v ./...\ntest:\n\tgo test ./...\n"}, nil, ""},
		"cargo runner":    {map[string]string{"Cargo.toml": "[package]\nname=\"x\"\n"}, map[string]string{".cargo/config.toml": "[target.x86_64-unknown-linux-gnu]\nrunner = \"true\"\n"}, nil, "block:.cargo/config.toml"},
		"cargo test off":  {map[string]string{"Cargo.toml": "[package]\nname=\"x\"\n\n[lib]\npath=\"src/lib.rs\"\n"}, map[string]string{"Cargo.toml": "[package]\nname=\"x\"\n\n[lib]\npath=\"src/lib.rs\"\ntest = false\n"}, nil, "block:Cargo.toml"},
		"cargo dep":       {map[string]string{"Cargo.toml": "[package]\nname=\"x\"\n\n[dependencies]\n"}, map[string]string{"Cargo.toml": "[package]\nname=\"x\"\n\n[dependencies]\nserde = \"1\"\n"}, nil, ""},
		"maven skip":      {map[string]string{"pom.xml": "<project><properties></properties></project>"}, map[string]string{"pom.xml": "<project><properties><skipTests>true</skipTests></properties></project>"}, nil, "block:pom.xml"},
		"maven config":    {map[string]string{"pom.xml": "<project/>"}, map[string]string{".mvn/maven.config": "-DskipTests\n"}, nil, "block:.mvn/maven.config"},
		"gradle ignore":   {map[string]string{"build.gradle.kts": "plugins { java }\n"}, map[string]string{"build.gradle.kts": "plugins { java }\ntasks.test { ignoreFailures = true }\n"}, nil, "block:build.gradle.kts"},
		"gradlew":         {map[string]string{"gradlew": "#!/bin/sh\nexec java -jar gradle/wrapper/gradle-wrapper.jar \"$@\"\n"}, map[string]string{"gradlew": "#!/bin/sh\nexit 0\n"}, nil, "block:gradlew"},
		"pytest addopts":  {map[string]string{"pyproject.toml": "[project]\nname='x'\n\n[tool.pytest.ini_options]\naddopts='-q'\n"}, map[string]string{"pyproject.toml": "[project]\nname='x'\n\n[tool.pytest.ini_options]\naddopts='-q -k nothing'\n"}, nil, "review:pyproject.toml"},
		"pyproject dep":   {map[string]string{"pyproject.toml": "[project]\nname='x'\ndependencies=[]\n\n[tool.pytest.ini_options]\naddopts='-q'\n"}, map[string]string{"pyproject.toml": "[project]\nname='x'\ndependencies=['attrs']\n\n[tool.pytest.ini_options]\naddopts='-q'\n"}, nil, ""},
		"conftest":        {map[string]string{"conftest.py": "import pytest\n"}, map[string]string{"conftest.py": "import pytest\ndef pytest_collection_modifyitems(items):\n    items.clear()\n"}, nil, "review:conftest.py"},
		"go skip":         {map[string]string{"a_test.go": "package a\nfunc TestA(t *testing.T) {\n\tcheck(t)\n}\n"}, map[string]string{"a_test.go": "package a\nfunc TestA(t *testing.T) {\n\tt.Skip(\"flaky\")\n\tcheck(t)\n}\n"}, nil, "review:a_test.go"},
		"go build tag":    {map[string]string{"a_test.go": "package a\n"}, map[string]string{"a_test.go": "//go:build ignore\n\npackage a\n"}, nil, "review:a_test.go"},
		"js only":         {map[string]string{"x.test.js": "test('a', () => {})\ntest('b', () => {})\n"}, map[string]string{"x.test.js": "test.only('a', () => {})\ntest('b', () => {})\n"}, nil, "review:x.test.js"},
		"java disabled":   {map[string]string{"src/test/java/ATest.java": "class ATest {\n  @Test void a() {}\n}\n"}, map[string]string{"src/test/java/ATest.java": "class ATest {\n  @Disabled @Test void a() {}\n}\n"}, nil, "review:src/test/java/ATest.java"},
		"rust ignore":     {map[string]string{"tests/it.rs": "#[test]\nfn a() {}\n"}, map[string]string{"tests/it.rs": "#[test]\n#[ignore]\nfn a() {}\n"}, nil, "review:tests/it.rs"},
		"python skip":     {map[string]string{"tests/test_a.py": "def test_a():\n    assert f() == 1\n"}, map[string]string{"tests/test_a.py": "import pytest\n@pytest.mark.skip\ndef test_a():\n    assert f() == 1\n"}, nil, "review:tests/test_a.py"},
		"assertions gone": {map[string]string{"tests/test_a.py": "def test_a():\n    assert f() == 1\n    assert g() == 2\n"}, map[string]string{"tests/test_a.py": "def test_a():\n    f()\n    g()\n"}, nil, "review:tests/test_a.py"},
		"assertion edit":  {map[string]string{"tests/test_a.py": "def test_a():\n    assert f() == 1\n"}, map[string]string{"tests/test_a.py": "def test_a():\n    assert f() == 2\n"}, nil, ""},
		"test deleted":    {map[string]string{"a_test.go": "package a\n", "a.go": "package a\n"}, map[string]string{"a.go": "package a\n// x\n"}, []string{"a_test.go"}, "review:a_test.go"},
		"test moved":      {map[string]string{"old/a_test.go": "package a\n", "a.go": "package a\n"}, map[string]string{"new/a_test.go": "package a\n"}, []string{"old/a_test.go"}, ""},
		"new tests":       {map[string]string{"a.go": "package a\n"}, map[string]string{"a_test.go": "package a\nfunc TestA(t *testing.T) {\n\tif f() != 1 {\n\t\tt.Fatal()\n\t}\n}\n"}, nil, ""},
		"npmrc":           {map[string]string{"package.json": pkg}, map[string]string{".npmrc": "script-shell=/bin/true\n"}, nil, "block:.npmrc"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := severities(integrityOf(t, c.base, c.change, c.remove...)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A blocking finding fails verification; a review finding does not.
func TestIntegrityStage(t *testing.T) {
	if st := integrityStage([]IntegrityFinding{{File: "x", Severity: IntegrityReview, Reason: "r"}}); st.Status != "pass" {
		t.Fatal(st)
	}
	if st := integrityStage([]IntegrityFinding{{File: "gradlew", Severity: IntegrityBlock, Reason: "r"}}); st.Status != "fail" || st.Cause != CauseIntegrity {
		t.Fatal(st)
	}
}
