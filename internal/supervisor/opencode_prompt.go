package supervisor

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/akynte/boundedcode/internal/artifacts"
	"github.com/akynte/boundedcode/internal/ledger"
	"github.com/akynte/boundedcode/internal/store"
)

// RecordOpenCodePrompt captures the original user bytes before OpenCode can
// compact them. The session namespace is provisional until bc_task_start binds
// the first prompt to a supervised task; it is never treated as a task ID.
func RecordOpenCodePrompt(ctx context.Context, st *store.Store, sessionID, messageID, body string) (string, error) {
	if !strings.HasPrefix(sessionID, "ses_") || len(sessionID) > 100 || !strings.HasPrefix(messageID, "msg_") || len(messageID) > 100 {
		return "", fmt.Errorf("invalid OpenCode prompt identity")
	}
	if len(body) == 0 || len(body) > 1<<20 {
		return "", fmt.Errorf("prompt must be 1..1048576 bytes")
	}
	hash, err := artifacts.New(st).Put([]byte(body))
	if err != nil {
		return "", err
	}
	intent := map[string]string{"session_id": sessionID, "message_id": messageID, "artifact": hash, "source": "user_prompt"}
	h, err := ledger.New(st).Begin(ctx, "prompt:"+sessionID, ledger.KindMemory, intent, "")
	if err != nil {
		return "", err
	}
	return hash, h.Complete(ctx, intent, "", hash)
}

// OriginalOpenCodePrompt returns the first captured prompt in the session.
// This is the user's raw request, rather than a model-written task title.
func OriginalOpenCodePrompt(ctx context.Context, st *store.Store, sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	var hash string
	err := st.Ledger().SQL().QueryRowContext(ctx, `SELECT json_extract(intent,'$.artifact') FROM operations WHERE task_id=? AND kind='memory' AND json_extract(intent,'$.source')='user_prompt' ORDER BY id LIMIT 1`, "prompt:"+sessionID).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}
