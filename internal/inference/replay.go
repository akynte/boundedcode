package inference

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ReplayStore keeps each assistant turn exactly as a provider returned it,
// so it can be sent back unchanged on later requests. Anthropic's thinking
// blocks and Gemini's thought signatures must be replayed verbatim, and the
// OpenAI chat format the agent keeps its history in has no place for them.
// The store holds them on the host; the agent never sees them.
//
// A turn is keyed by its tool-call ids, or by its text when it calls no
// tool (ReplayKey).
type ReplayStore interface {
	Get(key string) (json.RawMessage, bool)
	Put(key string, native json.RawMessage)
}

// ReplayKey is the key of an assistant turn: its sorted tool-call ids, or a
// hash of its text.
func ReplayKey(provider string, toolCallIDs []string, text string) string {
	if len(toolCallIDs) > 0 {
		ids := append([]string(nil), toolCallIDs...)
		sort.Strings(ids)
		return provider + ":calls:" + strings.Join(ids, ",")
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return provider + ":text:" + hex.EncodeToString(h[:16])
}

// MemoryReplay is an in-memory ReplayStore (tests, host-side calls).
type MemoryReplay struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

// Get implements ReplayStore.
func (r *MemoryReplay) Get(key string) (json.RawMessage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.m[key]
	return v, ok
}

// Put implements ReplayStore.
func (r *MemoryReplay) Put(key string, native json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]json.RawMessage{}
	}
	r.m[key] = native
}

// FileReplay is a ReplayStore persisted as JSON lines in a task directory,
// so turns survive a resume. The file is owner-only: thinking content can
// quote the repository.
type FileReplay struct {
	Path string

	once sync.Once
	mem  MemoryReplay
	mu   sync.Mutex
}

type replayLine struct {
	Key    string          `json:"k"`
	Native json.RawMessage `json:"v"`
}

func (r *FileReplay) load() {
	r.once.Do(func() {
		f, err := os.Open(r.Path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
		for sc.Scan() {
			var l replayLine
			if json.Unmarshal(sc.Bytes(), &l) == nil && l.Key != "" {
				r.mem.Put(l.Key, l.Native)
			}
		}
	})
}

// Get implements ReplayStore.
func (r *FileReplay) Get(key string) (json.RawMessage, bool) {
	r.load()
	return r.mem.Get(key)
}

// Put implements ReplayStore. A write failure only costs the replay (the
// turn is then rebuilt from the OpenAI history without native blocks).
func (r *FileReplay) Put(key string, native json.RawMessage) {
	r.load()
	r.mem.Put(key, native)
	b, err := json.Marshal(replayLine{Key: key, Native: native})
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(r.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}
