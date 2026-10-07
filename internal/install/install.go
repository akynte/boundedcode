// Package install downloads pinned release archives of BoundedCode's
// external tools (gitleaks, codebase-memory-mcp, llama.cpp), verifies them
// against a sha256 pinned here, and unpacks them. It replaces the bash
// installers so set-up works on Linux, macOS and Windows without bash,
// curl or a compiler.
package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Asset is one pinned download.
type Asset struct {
	URL    string
	SHA256 string
}

// Progress reports bytes done of total (total 0 = unknown).
type Progress func(done, total int64)

// Fetch downloads an asset into dir and verifies its sha256. The file is
// removed again when it does not match.
func Fetch(ctx context.Context, a Asset, dir string, progress Progress) (string, error) {
	if a.URL == "" || len(a.SHA256) != 64 {
		return "", errors.New("asset is not pinned")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: http %d", a.URL, resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, fn: progress}
	_, err = io.Copy(io.MultiWriter(f, h), pr)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", a.URL, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("download %s failed verification: sha256 %s, pinned %s", a.URL, got, a.SHA256)
	}
	return f.Name(), nil
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	fn    Progress
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.fn != nil && (time.Since(p.last) > 500*time.Millisecond || errors.Is(err, io.EOF)) {
		p.fn(p.done, p.total)
		p.last = time.Now()
	}
	return n, err
}

// Unpack extracts an archive (.tar.gz or .zip, by its name) into dest:
// regular files and symlinks that stay inside dest. A single top-level
// directory shared by every entry is stripped. keep, when set, limits the
// files written (by their base name).
func Unpack(archive, name, dest string, keep func(base string) bool) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	switch {
	case strings.HasSuffix(name, ".zip"):
		return unpackZip(archive, dest, keep)
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return unpackTarGz(archive, name, dest, keep)
	}
	return fmt.Errorf("%s: unknown archive type", name)
}

func unpackZip(archive, dest string, keep func(string) bool) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			names = append(names, f.Name)
		}
	}
	strip := commonTop(names)
	for _, f := range zr.File {
		rel := strings.TrimPrefix(f.Name, strip)
		if f.FileInfo().IsDir() || rel == "" || (keep != nil && !keep(path.Base(rel))) {
			continue
		}
		target, err := inside(dest, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// tarEntries calls fn for each entry of a .tar.gz.
func tarEntries(archive string, fn func(h *tar.Header, r io.Reader) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

func unpackTarGz(archive, name, dest string, keep func(string) bool) error {
	var names []string
	if err := tarEntries(archive, func(h *tar.Header, _ io.Reader) error {
		if h.Typeflag != tar.TypeDir {
			names = append(names, h.Name)
		}
		return nil
	}); err != nil {
		return err
	}
	strip := commonTop(names)
	return tarEntries(archive, func(h *tar.Header, r io.Reader) error {
		rel := strings.TrimPrefix(h.Name, strip)
		if h.Typeflag == tar.TypeDir || rel == "" || (keep != nil && !keep(path.Base(rel))) {
			return nil
		}
		target, err := inside(dest, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeSymlink:
			// Only links to a path inside the archive (library version
			// links): never absolute, never leaving dest.
			if path.IsAbs(h.Linkname) || strings.HasPrefix(path.Clean(path.Join(path.Dir(rel), h.Linkname)), "..") {
				return fmt.Errorf("%s: link %s -> %s leaves the archive", name, h.Name, h.Linkname)
			}
			_ = os.Remove(target)
			return os.Symlink(h.Linkname, target)
		case tar.TypeReg:
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			_ = os.Remove(target) // a running binary on macOS must be replaced, not rewritten
			return writeFile(target, r, mode)
		}
		return nil // devices, hard links: not in release archives
	})
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, 4<<30)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// commonTop is "top/" when every entry is under one top-level directory.
func commonTop(names []string) string {
	top := ""
	for _, n := range names {
		first, _, found := strings.Cut(strings.TrimPrefix(n, "./"), "/")
		if !found {
			return ""
		}
		if top == "" {
			top = first
		} else if top != first {
			return ""
		}
	}
	if top == "" {
		return ""
	}
	return top + "/"
}

// inside joins rel to dest and refuses paths that leave dest.
func inside(dest, rel string) (string, error) {
	rel = strings.TrimPrefix(rel, "./")
	if path.IsAbs(rel) || strings.Contains(rel, `\`) && filepath.Separator != '\\' {
		return "", fmt.Errorf("unsafe archive path %q", rel)
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("unsafe archive path %q", rel)
	}
	return filepath.Join(dest, filepath.FromSlash(clean)), nil
}
