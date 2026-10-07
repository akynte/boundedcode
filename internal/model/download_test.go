package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeHub serves one file with Range support and the tree API.
type fakeHub struct {
	data       []byte
	gated      bool
	ignoreRng  bool
	mu         sync.Mutex
	ranges     []string
	authHeader string
}

func (h *fakeHub) serve(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.ranges = append(h.ranges, r.Header.Get("Range"))
		h.authHeader = r.Header.Get("Authorization")
		h.mu.Unlock()
		if h.gated && r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256(h.data)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/models/org/repo/tree/"):
			fmt.Fprintf(w, `[{"path":"m.gguf","size":%d,"lfs":{"oid":"%s","size":%d}}]`, len(h.data), hex.EncodeToString(sum[:]), len(h.data))
		case r.URL.Path == "/org/repo/resolve/"+rev+"/m.gguf":
			body := h.data
			if rg := r.Header.Get("Range"); rg != "" && !h.ignoreRng {
				from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
				body = h.data[from:]
				w.WriteHeader(http.StatusPartialContent)
			}
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := HuggingFaceURL
	HuggingFaceURL = srv.URL
	t.Cleanup(func() { HuggingFaceURL = old })
}

const rev = "0123456789abcdef0123456789abcdef01234567"

func profileFor(data []byte, withHash bool) Profile {
	p := Profile{Name: "m", File: "m.gguf", Source: Source{Repo: "org/repo", File: "m.gguf", Revision: rev}}
	if withHash {
		sum := sha256.Sum256(data)
		p.Source.SHA256, p.Source.SizeBytes = hex.EncodeToString(sum[:]), int64(len(data))
	}
	return p
}

func TestDownloadResumeAndVerify(t *testing.T) {
	data := []byte(strings.Repeat("gguf-weights-", 100000))
	hub := &fakeHub{data: data}
	hub.serve(t)
	dir := t.TempDir()
	p := profileFor(data, true)
	// A previous run left half the file.
	half := len(data) / 2
	if err := os.WriteFile(filepath.Join(dir, "m.gguf.part"), data[:half], 0o644); err != nil {
		t.Fatal(err)
	}
	var last int64
	d := &Downloader{Progress: func(done, _ int64) { last = done }}
	path, err := d.Fetch(context.Background(), p, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) || last != int64(len(data)) {
		t.Fatalf("file %d bytes, progress %d", len(got), last)
	}
	if hub.ranges[0] != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("resume range = %q", hub.ranges[0])
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatal("part file left")
	}
	// A second fetch keeps the verified file (no download).
	hub.ranges = nil
	if _, err := d.Fetch(context.Background(), p, dir); err != nil || len(hub.ranges) != 0 {
		t.Fatalf("refetch: %v, requests %v", err, hub.ranges)
	}
}

func TestDownloadRestartsWhenRangeIgnored(t *testing.T) {
	data := []byte(strings.Repeat("x", 4096))
	hub := &fakeHub{data: data, ignoreRng: true}
	hub.serve(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.gguf.part"), data[:1000], 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := (&Downloader{}).Fetch(context.Background(), profileFor(data, true), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Fatalf("got %d bytes", len(got))
	}
}

func TestDownloadRejectsWrongHash(t *testing.T) {
	data := []byte("real weights")
	hub := &fakeHub{data: data}
	hub.serve(t)
	dir := t.TempDir()
	p := profileFor(data, true)
	p.Source.SHA256 = strings.Repeat("0", 64)
	if _, err := (&Downloader{}).Fetch(context.Background(), p, dir); err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "m.gguf.part")); !os.IsNotExist(err) {
		t.Fatal("unverified part file kept")
	}
}

func TestDownloadHashFromAPIAndGated(t *testing.T) {
	data := []byte("weights from a profile without a recorded hash")
	hub := &fakeHub{data: data, gated: true}
	hub.serve(t)
	p := profileFor(data, false)
	if _, err := (&Downloader{}).Fetch(context.Background(), p, t.TempDir()); !errors.Is(err, ErrGated) {
		t.Fatalf("gated without token: %v", err)
	}
	path, err := (&Downloader{Token: "hf_test"}).Fetch(context.Background(), p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) || hub.authHeader != "Bearer hf_test" {
		t.Fatalf("got %q auth %q", got, hub.authHeader)
	}
}

func TestDownloadNeedsPinnedRevision(t *testing.T) {
	p := Profile{Name: "m", File: "m.gguf", Source: Source{Repo: "org/repo", File: "m.gguf", Revision: "main"}}
	if _, err := (&Downloader{}).Fetch(context.Background(), p, t.TempDir()); err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("err = %v", err)
	}
}
