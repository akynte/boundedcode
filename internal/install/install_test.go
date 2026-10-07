package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tarGz(t *testing.T, entries []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h := h
		if b, ok := bodies[h.Name]; ok {
			h.Size = int64(len(b))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if b, ok := bodies[h.Name]; ok {
			_, _ = tw.Write([]byte(b))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestFetchAndUnpackTarGz(t *testing.T) {
	data := tarGz(t, []tar.Header{
		{Name: "llama-b1/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "llama-b1/llama-server", Typeflag: tar.TypeReg, Mode: 0o755},
		{Name: "llama-b1/libggml.0.25.1.dylib", Typeflag: tar.TypeReg, Mode: 0o755},
		{Name: "llama-b1/libggml.0.dylib", Typeflag: tar.TypeSymlink, Linkname: "libggml.0.25.1.dylib"},
		{Name: "llama-b1/LICENSE", Typeflag: tar.TypeReg, Mode: 0o644},
	}, map[string]string{"llama-b1/llama-server": "#!bin", "llama-b1/libggml.0.25.1.dylib": "lib", "llama-b1/LICENSE": "MIT"})
	sum := sha256.Sum256(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer srv.Close()
	dir := t.TempDir()
	var last int64
	path, err := Fetch(context.Background(), Asset{srv.URL + "/a.tar.gz", hex.EncodeToString(sum[:])}, dir, func(d, _ int64) { last = d })
	if err != nil || last != int64(len(data)) {
		t.Fatalf("fetch: %v progress %d", err, last)
	}
	dest := filepath.Join(dir, "bin")
	if err := Unpack(path, "a.tar.gz", dest, nil); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dest, "llama-server"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("server not executable: %v %v", fi, err)
	}
	if target, err := os.Readlink(filepath.Join(dest, "libggml.0.dylib")); err != nil || target != "libggml.0.25.1.dylib" {
		t.Fatalf("library link: %q %v", target, err)
	}
	// A wrong digest is refused and leaves nothing behind.
	if _, err := Fetch(context.Background(), Asset{srv.URL + "/a.tar.gz", strings.Repeat("0", 64)}, dir, nil); err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("bad digest: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "download-*")); len(left) != 1 { // the first, good download
		t.Fatalf("downloads left: %v", left)
	}
}

func TestUnpackRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	for name, h := range map[string]tar.Header{
		"dotdot": {Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644},
		"link":   {Name: "x/lib", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		"abs":    {Name: "y/lib", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		data := tarGz(t, []tar.Header{h, {Name: "z/ok", Typeflag: tar.TypeReg}}, map[string]string{h.Name: "x", "z/ok": "x"})
		p := filepath.Join(dir, name+".tar.gz")
		_ = os.WriteFile(p, data, 0o644)
		if err := Unpack(p, name+".tar.gz", filepath.Join(dir, "out-"+name), nil); err == nil {
			t.Errorf("%s: unsafe entry extracted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil")); err == nil {
		t.Fatal("file written outside dest")
	}
}

func TestUnpackZipKeep(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range []string{"gitleaks.exe", "README.md", "LICENSE"} {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(n))
	}
	zw.Close()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.zip")
	_ = os.WriteFile(p, buf.Bytes(), 0o644)
	if err := Unpack(p, "g.zip", filepath.Join(dir, "bin"), func(b string) bool { return b == "gitleaks.exe" }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "gitleaks.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "README.md")); err == nil {
		t.Fatal("unwanted file extracted")
	}
}

func TestPins(t *testing.T) {
	for _, m := range []map[string]Asset{Gitleaks, CBM} {
		for p, a := range m {
			if len(a.SHA256) != 64 || !strings.HasPrefix(a.URL, "https://github.com/") {
				t.Errorf("%s: %+v", p, a)
			}
		}
	}
	if v, ok := PickLlama("windows/amd64", true); !ok || v.Name != "cuda" || v.Runtime == nil {
		t.Fatalf("windows nvidia: %+v", v)
	}
	if v, ok := PickLlama("windows/amd64", false); !ok || v.Name != "cpu" {
		t.Fatalf("windows cpu: %+v", v)
	}
	if v, ok := PickLlama("darwin/arm64", false); !ok || v.Name != "metal" {
		t.Fatalf("mac: %+v", v)
	}
	if _, ok := PickLlama("plan9/386", false); ok {
		t.Fatal("unknown platform")
	}
}
