package execbox

import (
	"net"
	"testing"
)

func TestHostAllowed(t *testing.T) {
	allow := []string{"api.example.com", ".pkg.example.org", "*.cdn.example.net"}
	for host, want := range map[string]bool{
		"api.example.com": true, "API.example.com.": true, "evil.example.com": false, "example.com": false,
		"pkg.example.org": true, "a.pkg.example.org": true, "pkg.example.org.evil.com": false, "x.cdn.example.net": true,
		"cdn.example.net": false,
	} {
		if got := hostAllowed(allow, host); got != want {
			t.Errorf("%s: %v", host, got)
		}
	}
}

func TestPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{"93.184.216.34": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false,
		"169.254.169.254": false, "::1": false, "fd00::1": false, "100.64.0.1": false, "0.0.0.0": false, "2606:4700::1111": true} {
		if got := publicIP(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
}
