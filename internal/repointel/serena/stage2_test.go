package serena

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStage2SymbolEdits is the Stage 2 experiment of the editing policy
// (ADR-0008): it measures Serena's symbol-level edits on the fixtures —
// latency, whether the diff stays scoped, whether the result builds and is
// gofmt-clean, and whether verification catches a broken edit. Editing is
// not enabled in the product. Run with BC_SERENA_STAGE2=1; set
// BC_STAGE2_REPORT=path to write the markdown table.
func TestStage2SymbolEdits(t *testing.T) {
	exe := realSerena(t)
	if os.Getenv("BC_SERENA_STAGE2") == "" {
		t.Skip("set BC_SERENA_STAGE2=1")
	}
	type edit struct {
		name, repo, tool string
		args             map[string]any
		wantFiles        []string // exactly these files may change
		check            []string // command that must pass afterwards ("" = must fail)
		mustFail         bool
	}
	cancelPG := "\n// Cancel runs: UPDATE payments SET status = 'cancelled' WHERE id = $1\nfunc (r *PostgresRepository) Cancel(_ context.Context, id string) error {\n\tr.mu.Lock()\n\tdefer r.mu.Unlock()\n\tp, ok := r.rows[id]\n\tif !ok {\n\t\treturn ErrNotFound\n\t}\n\tp.Status = \"cancelled\"\n\tr.rows[id] = p\n\treturn nil\n}\n"
	cancelMem := "\n// Cancel marks the payment cancelled.\nfunc (m *MemoryRepository) Cancel(_ context.Context, id string) error {\n\tfor i := range m.items {\n\t\tif m.items[i].ID == id {\n\t\t\tm.items[i].Status = \"cancelled\"\n\t\t\treturn nil\n\t\t}\n\t}\n\treturn ErrNotFound\n}\n"
	iface := "type PaymentRepository interface {\n\t// Insert stores a new payment.\n\tInsert(ctx context.Context, p Payment) error\n\t// Get loads a payment by id.\n\tGet(ctx context.Context, id string) (Payment, error)\n\t// Cancel marks a payment cancelled.\n\tCancel(ctx context.Context, id string) error\n}"
	cancelSvc := "\n// CancelPayment cancels a payment.\nfunc (s *Service) CancelPayment(ctx context.Context, id string) error {\n\treturn s.repo.Cancel(ctx, id)\n}\n"
	gobuild := []string{"sh", "-c", "go build ./... && go vet ./... && go test ./..."}
	tsc := []string{"npx", "-y", "-p", "typescript@5.9.3", "tsc", "--noEmit", "-p", "."}
	steps := []edit{
		{"add method to PostgresRepository", "billing-service", "insert_after_symbol",
			map[string]any{"name_path": "Get", "relative_path": "internal/payment/postgres.go", "body": cancelPG},
			[]string{"internal/payment/postgres.go"}, gobuild, false},
		{"add method to MemoryRepository", "billing-service", "insert_after_symbol",
			map[string]any{"name_path": "Get", "relative_path": "internal/payment/memory.go", "body": cancelMem},
			[]string{"internal/payment/memory.go"}, gobuild, false},
		{"extend PaymentRepository interface", "billing-service", "replace_symbol_body",
			map[string]any{"name_path": "PaymentRepository", "relative_path": "internal/payment/repository.go", "body": iface},
			[]string{"internal/payment/repository.go"}, gobuild, false},
		{"add Service.CancelPayment", "billing-service", "insert_after_symbol",
			map[string]any{"name_path": "CreatePayment", "relative_path": "internal/payment/service.go", "body": cancelSvc},
			[]string{"internal/payment/service.go"}, gobuild, false},
		{"rename CreatePayment (Go, cross-file)", "billing-service", "rename_symbol",
			map[string]any{"name_path": "CreatePayment", "relative_path": "internal/payment/service.go", "new_name": "OpenPayment"},
			[]string{"internal/payment/service.go", "internal/api/handler.go", "internal/payment/service_test.go"}, gobuild, false},
		{"broken body is caught by verification", "billing-service", "replace_symbol_body",
			map[string]any{"name_path": "GetPayment", "relative_path": "internal/payment/service.go", "body": "func (s *Service) GetPayment(ctx context.Context, id string) (Payment, error) {\n\treturn s.repo.Fetch(ctx, id)\n}"},
			[]string{"internal/payment/service.go"}, gobuild, true},
		{"rename submitPayment (TS, cross-file, cold server)", "web-checkout", "rename_symbol",
			map[string]any{"name_path": "submitPayment", "relative_path": "src/services/paymentService.ts", "new_name": "chargePayment"},
			[]string{"src/services/paymentService.ts", "src/api/checkoutController.ts"}, tsc, false},
		{"(read-only) references of submitPayment, warming the server", "web-checkout", "find_referencing_symbols",
			map[string]any{"name_path": "submitPayment", "relative_path": "src/services/paymentService.ts"},
			nil, tsc, false},
		{"rename submitPayment (TS, cross-file, warm server)", "web-checkout", "rename_symbol",
			map[string]any{"name_path": "submitPayment", "relative_path": "src/services/paymentService.ts", "new_name": "chargePayment"},
			[]string{"src/services/paymentService.ts", "src/api/checkoutController.ts"}, tsc, false},
		{"replace TS function body", "web-checkout", "replace_symbol_body",
			map[string]any{"name_path": "validateRequest", "relative_path": "src/services/paymentService.ts",
				"body": "export function validateRequest(req: PaymentRequest): string | null {\n  if (!req.accountId) return \"accountId is required\";\n  if (!Number.isInteger(req.amountCents) || req.amountCents <= 0) return \"amountCents must be a positive integer\";\n  if (!/^[A-Z]{3}$/.test(req.currency)) return \"currency must be a 3-letter code\";\n  return null;\n}"},
			[]string{"src/services/paymentService.ts"}, tsc, false},
	}
	repos := map[string]string{"billing-service": fixtureRepo(t, "billing-service"), "web-checkout": fixtureRepo(t, "web-checkout")}
	m := newRealManager(t, exe)
	m.editing = true
	var rows []string
	ok := 0
	for _, e := range steps {
		root := repos[e.repo]
		before := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
		p := (&Navigator{M: m}).project(root)
		t0 := time.Now()
		out, err := m.Call(context.Background(), p, e.tool, e.args)
		ms := time.Since(t0).Milliseconds()
		changed := strings.Fields(git(t, root, "diff", "--name-only"))
		if len(changed) == 0 && len(e.wantFiles) == 0 {
			changed = nil
		}
		numstat := strings.TrimSpace(git(t, root, "diff", "--shortstat"))
		scoped := sameSet(changed, e.wantFiles)
		cmd := exec.Command(e.check[0], e.check[1:]...)
		cmd.Dir = root
		cout, cerr := cmd.CombinedOutput()
		built := cerr == nil
		fmtClean := true
		if e.repo == "billing-service" {
			g, _ := exec.Command("gofmt", "-l", root).Output()
			fmtClean = len(strings.TrimSpace(string(g))) == 0
		}
		pass := err == nil && scoped && built != e.mustFail
		if pass {
			ok++
		}
		note := ""
		if err != nil {
			t.Errorf("%s: %v", e.name, err)
			note = "tool error: " + firstLine(err.Error())
		} else if !built && !e.mustFail {
			note = "check failed: " + lastN(strings.TrimSpace(string(cout)), 160)
		}
		_ = out
		rows = append(rows, fmt.Sprintf("| %s | %s | %d | %v | %s | %v | %v | %v | %s |", e.name, e.tool, ms, scoped,
			strings.ReplaceAll(numstat, "|", "/"), built, fmtClean, pass, strings.ReplaceAll(strings.ReplaceAll(note, "\n", " "), "|", "/")))
		// Commit (or revert a broken edit) so steps are independent.
		if e.mustFail || !pass {
			git(t, root, "checkout", "--", ".")
		} else {
			git(t, root, "add", "-A")
			git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", e.name)
		}
		_ = before
	}
	table := "| edit | tool | ms | diff scoped | diff | builds/typechecks | gofmt clean | as expected | note |\n|---|---|---|---|---|---|---|---|---|\n" +
		strings.Join(rows, "\n") + fmt.Sprintf("\n\n%d/%d edits as expected.\n", ok, len(steps))
	t.Log("\n" + table)
	if p := os.Getenv("BC_STAGE2_REPORT"); p != "" {
		_ = os.WriteFile(p, []byte(table), 0o644)
	}
	if ws, _ := filepath.Glob(filepath.Join(repos["billing-service"], ".serena")); len(ws) > 0 {
		t.Fatal("serena wrote into the repository")
	}
}

func sameSet(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}
