package direct

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxImageBytes  = 4 << 20
	maxAttachBytes = 64 << 10
)

var imageMimes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

func imageMime(p string) string { return imageMimes[strings.ToLower(filepath.Ext(p))] }

var atRef = regexp.MustCompile(`(^|\s)@([\w./~-][\w./-]*)`)

// attach turns @path references in a message into attached file contents
// (text) or inline images, like other agents' @file syntax. Paths must be
// inside the project; unknown references are left as typed.
func (r *Runner) attach(text string) Message {
	m := Message{Role: "user", Content: text}
	var parts []map[string]any
	var files []string
	for _, match := range atRef.FindAllStringSubmatch(text, -1) {
		p := strings.TrimRight(match[2], ".,;:)")
		abs, rel, err := r.resolve(p)
		if err != nil || r.checkReadable(abs) != nil {
			continue
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			continue
		}
		if mime := imageMime(abs); mime != "" && st.Size() <= maxImageBytes {
			if b, err := os.ReadFile(abs); err == nil {
				parts = append(parts, imagePart(mime, b))
				files = append(files, rel+" (image)")
			}
			continue
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if len(b) > maxAttachBytes {
			b = append(b[:maxAttachBytes], []byte("\n… (truncated; use read with an offset for the rest)")...)
		}
		files = append(files, rel)
		m.Content += fmt.Sprintf("\n\n<file path=%q>\n%s\n</file>", rel, strings.TrimRight(string(b), "\n"))
	}
	if len(parts) > 0 {
		m.Parts = append([]map[string]any{{"type": "text", "text": m.Content}}, parts...)
	}
	if len(files) > 0 {
		r.emit(Event{Kind: EventNote, Text: "attached " + strings.Join(files, ", ")})
	}
	return m
}

func imagePart(mime string, data []byte) map[string]any {
	return map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}}
}

// attachImage queues an image read by a tool: it is added as a user
// message after the tool results, since tool results are text.
func (r *Runner) attachImage(rel, mime string, data []byte) {
	r.pendingImages = append(r.pendingImages, Message{Role: "user", Parts: []map[string]any{
		{"type": "text", "text": "The image " + rel + " read by the tool:"}, imagePart(mime, data)}, Provenance: untrusted(SourceRepository, rel)})
}
