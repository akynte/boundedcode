package sandbox

import "testing"

func TestContainerPathFor(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\me\repo`:         "/host/c/Users/me/repo",
		`d:/data/boundedcode\work`: "/host/d/data/boundedcode/work",
		`C:\`:                      "/host/c",
		`\\nas\share\repo`:         "/host/unc/nas/share/repo",
	} {
		if got := containerPathFor("windows", in); got != want {
			t.Errorf("windows %q = %q, want %q", in, got, want)
		}
	}
	if got := containerPathFor("linux", "/home/me/repo"); got != "/home/me/repo" {
		t.Errorf("linux = %q", got)
	}
	if got := containerPathFor("darwin", "/Users/me/repo"); got != "/Users/me/repo" {
		t.Errorf("darwin = %q", got)
	}
}
