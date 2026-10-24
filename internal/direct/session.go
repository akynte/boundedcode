package direct

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session is one direct-mode conversation. It is persisted as newline-
// delimited JSON: a header line, then one line per message, appended as
// the conversation goes, so a crash loses nothing that was said.
type Session struct {
	ID       string    `json:"id"`
	Root     string    `json:"root"`
	Title    string    `json:"title"`
	Model    string    `json:"model"`
	Provider string    `json:"provider"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	// Compactions counts how often the history was summarised.
	Compactions int `json:"compactions,omitempty"`

	Messages []Message `json:"-"`
	// Path is the file the session is appended to ("" = in memory).
	Path string `json:"-"`
}

type sessionLine struct {
	Type    string   `json:"type"` // "session" | "message" | "compact"
	Session *Session `json:"session,omitempty"`
	Message *Message `json:"message,omitempty"`
	// Keep is the number of trailing messages kept after a compaction;
	// the summary message follows as a normal message line.
	Keep int `json:"keep,omitempty"`
}

// NewSession creates a session for root, stored under dir (the sessions
// directory; "" keeps it in memory).
func NewSession(dir, root, provider, model string) (*Session, error) {
	now := time.Now().UTC()
	s := &Session{ID: now.Format("20060102-150405") + "-" + shortHash(root + now.String())[:4], Root: root, Model: model, Provider: provider, Created: now, Updated: now}
	if dir == "" {
		return s, nil
	}
	pdir := filepath.Join(dir, ProjectKey(root))
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		return nil, err
	}
	s.Path = filepath.Join(pdir, s.ID+".jsonl")
	if err := s.writeLine(sessionLine{Type: "session", Session: s}); err != nil {
		return nil, err
	}
	return s, nil
}

// ProjectKey names the per-project sessions directory.
func ProjectKey(root string) string {
	base := filepath.Base(root)
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, base)
	return base + "-" + shortHash(filepath.Clean(root))[:8]
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Append records a message.
func (s *Session) Append(m Message) error {
	s.Messages = append(s.Messages, m)
	s.Updated = time.Now().UTC()
	if s.Path == "" {
		return nil
	}
	return s.writeLine(sessionLine{Type: "message", Message: &m})
}

// replace installs a compacted history: summary, then the kept tail.
func (s *Session) replace(summary Message, keep []Message) error {
	s.Messages = append([]Message{summary}, keep...)
	s.Compactions++
	s.Updated = time.Now().UTC()
	if s.Path == "" {
		return nil
	}
	if err := s.writeLine(sessionLine{Type: "compact", Keep: len(keep)}); err != nil {
		return err
	}
	return s.writeLine(sessionLine{Type: "message", Message: &summary})
}

func (s *Session) writeLine(l sessionLine) error {
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// LoadSession reads a session file back.
func LoadSession(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s *Session
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	n := 0
	summaryNext := false // the message after a "compact" line is the summary, which goes first
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var l sessionLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n, err)
		}
		switch l.Type {
		case "session":
			if l.Session == nil {
				return nil, fmt.Errorf("%s line %d: empty session header", path, n)
			}
			s = l.Session
			s.Path = path
		case "message":
			if s == nil || l.Message == nil {
				return nil, fmt.Errorf("%s line %d: message before header", path, n)
			}
			if summaryNext {
				s.Messages = append([]Message{*l.Message}, s.Messages...)
				summaryNext = false
			} else {
				s.Messages = append(s.Messages, *l.Message)
			}
		case "compact":
			if s == nil {
				return nil, fmt.Errorf("%s line %d: compaction before header", path, n)
			}
			if l.Keep < len(s.Messages) {
				s.Messages = append([]Message(nil), s.Messages[len(s.Messages)-l.Keep:]...)
			}
			s.Compactions++
			summaryNext = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, errors.New(path + ": not a session file")
	}
	if st, err := os.Stat(path); err == nil {
		s.Updated = st.ModTime().UTC()
	}
	if s.Title == "" {
		for _, m := range s.Messages {
			if m.Role == "user" && !strings.HasPrefix(m.Content, "[Earlier conversation") {
				s.Title = title(m.Content)
				break
			}
		}
	}
	return s, nil
}

// SessionInfo summarises a stored session.
type SessionInfo struct {
	ID       string
	Path     string
	Title    string
	Model    string
	Provider string
	Updated  time.Time
	Messages int
}

// ListSessions lists the stored sessions of root, newest first.
func ListSessions(dir, root string) ([]SessionInfo, error) {
	pdir := filepath.Join(dir, ProjectKey(root))
	entries, err := os.ReadDir(pdir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		p := filepath.Join(pdir, e.Name())
		s, err := LoadSession(p)
		if err != nil {
			continue
		}
		n := 0
		for _, m := range s.Messages {
			if m.Role == "user" {
				n++
			}
		}
		out = append(out, SessionInfo{ID: s.ID, Path: p, Title: s.Title, Model: s.Model, Provider: s.Provider, Updated: s.Updated, Messages: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// FindSession resolves an id (or unique prefix) to a session path; "" or
// "last" means the most recent.
func FindSession(dir, root, id string) (string, error) {
	list, err := ListSessions(dir, root)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errors.New("no stored sessions for " + root)
	}
	if id == "" || id == "last" {
		return list[0].Path, nil
	}
	var hits []SessionInfo
	for _, s := range list {
		if s.ID == id {
			return s.Path, nil
		}
		if strings.HasPrefix(s.ID, id) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no session %q", id)
	case 1:
		return hits[0].Path, nil
	}
	return "", fmt.Errorf("%q matches %d sessions", id, len(hits))
}

// Markdown renders the session as a readable transcript (the /export
// command): user and assistant messages in full, tool calls as one line
// each with their results clipped.
func (s *Session) Markdown() string {
	var b strings.Builder
	title := s.Title
	if title == "" {
		title = "BoundedCode session"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if s.ID != "" {
		fmt.Fprintf(&b, "Session %s", s.ID)
		if s.Model != "" {
			fmt.Fprintf(&b, " · %s/%s", s.Provider, s.Model)
		}
		if !s.Created.IsZero() {
			fmt.Fprintf(&b, " · %s", s.Created.Local().Format("2006-01-02 15:04"))
		}
		b.WriteString("\n\n")
	}
	for _, m := range s.Messages {
		switch m.Role {
		case "user":
			text := m.Content
			if text == "" && len(m.Parts) > 0 {
				text = "(image)"
			}
			fmt.Fprintf(&b, "## User\n\n%s\n\n", strings.TrimSpace(text))
		case "assistant":
			if strings.TrimSpace(m.Content) != "" {
				fmt.Fprintf(&b, "## Assistant\n\n%s\n\n", strings.TrimSpace(m.Content))
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "- 🛠 `%s` %s\n", tc.Function.Name, oneLine(tc.Function.Arguments, 200))
			}
			if len(m.ToolCalls) > 0 {
				b.WriteString("\n")
			}
		case "tool":
			out := strings.TrimSpace(m.Content)
			if len(out) > 1200 {
				out = out[:1200] + "\n…"
			}
			fmt.Fprintf(&b, "<details><summary>result of %s</summary>\n\n```\n%s\n```\n\n</details>\n\n", m.Name, out)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
