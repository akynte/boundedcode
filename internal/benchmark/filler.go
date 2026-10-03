package benchmark

import (
	"fmt"
	"strings"
)

// fillerText returns deterministic, code-like text of roughly n characters.
// Code-like text gives tokenization density close to real agent prompts.
func fillerText(n int, seed string) string {
	var b strings.Builder
	b.Grow(n + 256)
	fmt.Fprintf(&b, "// benchmark context %s\npackage service\n\n", seed)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, `// Handle%[1]d processes request %[1]d for tenant %[2]s.
func (s *Server) Handle%[1]d(ctx context.Context, req *Request%[1]d) (*Response%[1]d, error) {
	if err := s.validate%[3]d(req); err != nil {
		return nil, fmt.Errorf("handle%[1]d: invalid request: %%w", err)
	}
	row, err := s.store.Get(ctx, "accounts", req.ID+%[4]d)
	if err != nil {
		return nil, err
	}
	return &Response%[1]d{ID: row.ID, Balance: row.Balance * %[5]d}, nil
}

`, i, seed, i%17, i*7919%1000, i%13+1)
	}
	return b.String()[:n]
}
