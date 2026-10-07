package frontier

import (
	"strings"
	"testing"
)

// TestSanitizeWindowsPaths: a Windows home appears in tool output with
// either slash, JSON-escaped, in any case, and as the sandbox mounts it.
func TestSanitizeWindowsPaths(t *testing.T) {
	home := `C:\Users\Dev`
	repo := `C:\Users\Dev\src\orders`
	text := strings.Join([]string{
		`open C:\Users\Dev\src\orders\main.go: denied`,
		`at c:/users/dev/src/orders/x_test.go:12`,
		`{"file":"C:\\Users\\Dev\\src\\orders\\a.go"}`,
		`--- FAIL: /host/c/Users/Dev/src/orders/b_test.go:3`,
		`cache C:\Users\Dev\AppData\Local\go-build`,
		`other C:\Users\Developer\x stays`,
	}, "\n")
	got := Sanitize(text, PathMap{repo: "orders"}, home)
	for _, leak := range []string{`C:\Users\Dev\`, "c:/users/dev/", `C:\\Users\\Dev`, "/host/c/Users/Dev/"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(leak)) {
			t.Fatalf("leaked %q in:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, `orders\main.go`) || !strings.Contains(got, `$HOME\AppData`) || !strings.Contains(got, `C:\Users\Developer\x`) {
		t.Fatalf("sanitized:\n%s", got)
	}
	if err := CheckPacket(got, home); err != nil {
		t.Fatalf("check after sanitizing: %v", err)
	}
	if err := CheckPacket("see /host/c/users/dev/notes", home); err == nil {
		t.Fatal("the sandbox spelling of the home was not caught")
	}
}
