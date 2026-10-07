package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/akynte/boundedcode/internal/hw"
)

// HuggingFaceURL is the Hugging Face Hub endpoint.
var HuggingFaceURL = "https://huggingface.co"

// Progress reports a download: bytes done of total (total 0 = unknown).
type Progress func(done, total int64)

// Downloader fetches model weights from the Hugging Face Hub at a pinned
// revision, resumes interrupted downloads, and verifies the sha256.
type Downloader struct {
	HTTP *http.Client
	// Token is a Hugging Face access token for gated repositories ("" =
	// none). It is sent only to the Hub; redirects to other hosts drop it.
	Token string
	// Progress, when set, is called as bytes arrive (at most a few times a
	// second).
	Progress Progress
}

// ErrGated means the repository needs an accepted license and a token.
var ErrGated = errors.New("the model repository requires accepting its terms and a Hugging Face token")

// Path returns where a profile's weights are stored in dir.
func Path(p Profile, dir string) string {
	if filepath.IsAbs(p.File) {
		return p.File
	}
	return filepath.Join(dir, p.File)
}

// Fetch downloads p's weights into dir and returns the file path. An
// existing file with the expected size and hash is kept.
func (d *Downloader) Fetch(ctx context.Context, p Profile, dir string) (string, error) {
	src := p.Source
	if src.Repo == "" || src.File == "" {
		return "", fmt.Errorf("model %s: no download source in its profile", p.Name)
	}
	if !commitRE.MatchString(src.Revision) {
		return "", fmt.Errorf("model %s: source.revision must be a commit sha (got %q): downloads are pinned", p.Name, src.Revision)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := Path(p, dir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	want := strings.ToLower(src.SHA256)
	size := src.SizeBytes
	if want == "" || size <= 0 {
		var err error
		if want, size, err = d.lfsInfo(ctx, src); err != nil {
			return "", err
		}
	}
	if fi, err := os.Stat(dest); err == nil && fi.Size() == size {
		got, err := fileSHA256(dest)
		if err != nil {
			return "", err
		}
		if got == want {
			return dest, nil
		}
		return "", fmt.Errorf("%s exists but its sha256 is %s, not %s: remove it and fetch again", dest, got, want)
	}
	part := dest + ".part"
	have := int64(0)
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
		if have > size {
			_ = os.Remove(part)
			have = 0
		}
	}
	if free, err := hw.FreeDiskMiB(dir); err == nil {
		needMiB := int((size-have)>>20) + 512
		if free < needMiB {
			return "", fmt.Errorf("not enough disk space in %s: %.1f GB free, %.1f GB needed", dir, float64(free)/1024, float64(needMiB)/1024)
		}
	}
	h := sha256.New()
	if have > 0 {
		if err := hashFile(h, part); err != nil {
			return "", err
		}
	}
	if err := d.download(ctx, src, part, have, size, h); err != nil {
		return "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		_ = os.Remove(part)
		return "", fmt.Errorf("download of %s failed verification: sha256 %s, expected %s (the partial file was removed)", src.File, got, want)
	}
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (d *Downloader) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{}
}

func (d *Downloader) request(ctx context.Context, method, u string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	if d.Token != "" {
		// net/http drops Authorization on redirects to another host, so the
		// token does not reach the CDN the Hub redirects to.
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	return req, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// download appends the bytes from offset have to part, hashing them.
func (d *Downloader) download(ctx context.Context, src Source, part string, have, size int64, h hash.Hash) error {
	u := fmt.Sprintf("%s/%s/resolve/%s/%s", HuggingFaceURL, escapePath(src.Repo), src.Revision, escapePath(src.File))
	req, err := d.request(ctx, http.MethodGet, u)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", src.File, err)
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0:
		flags |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		// The server ignored the range: start over.
		have = 0
		h.Reset()
		flags |= os.O_TRUNC
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s: %w (accept them on https://huggingface.co/%s and store a token)", src.Repo, ErrGated, src.Repo)
	default:
		return fmt.Errorf("download %s: http %d", src.File, resp.StatusCode)
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := io.MultiWriter(f, h)
	buf := make([]byte, 1<<20)
	done := have
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if d.Progress != nil && time.Since(last) > 250*time.Millisecond {
				d.Progress(done, size)
				last = time.Now()
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return fmt.Errorf("download %s interrupted at %d of %d bytes (run the fetch again to resume): %w", src.File, done, size, rerr)
		}
	}
	if d.Progress != nil {
		d.Progress(done, size)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if done != size {
		return fmt.Errorf("download %s ended at %d of %d bytes (run the fetch again to resume)", src.File, done, size)
	}
	return nil
}

// lfsInfo reads a file's sha256 and size from the Hub API.
func (d *Downloader) lfsInfo(ctx context.Context, src Source) (string, int64, error) {
	u := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", HuggingFaceURL, escapePath(src.Repo), src.Revision)
	req, err := d.request(ctx, http.MethodGet, u)
	if err != nil {
		return "", 0, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", 0, fmt.Errorf("%s: %w", src.Repo, ErrGated)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("%s: file list: http %d", src.Repo, resp.StatusCode)
	}
	var entries []struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
		LFS  *struct {
			OID  string `json:"oid"`
			Size int64  `json:"size"`
		} `json:"lfs"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&entries); err != nil {
		return "", 0, fmt.Errorf("%s: file list: %w", src.Repo, err)
	}
	for _, e := range entries {
		if e.Path == src.File && e.LFS != nil && e.LFS.OID != "" {
			return strings.ToLower(e.LFS.OID), e.LFS.Size, nil
		}
	}
	return "", 0, fmt.Errorf("%s has no LFS file %s at %s", src.Repo, src.File, src.Revision)
}

func fileSHA256(path string) (string, error) {
	h := sha256.New()
	if err := hashFile(h, path); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(h hash.Hash, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(h, f)
	return err
}
