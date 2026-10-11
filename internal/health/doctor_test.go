package health

import "testing"

func TestSubstitute(t *testing.T) {
	vars := map[string]string{"exit_ip": "1.2.3.4", "iface": "AWG2"}
	got, miss := Substitute("curl -w '%{http_code}' --interface {iface} x | grep {exit_ip}", vars)
	if miss != "" || got != "curl -w '%{http_code}' --interface AWG2 x | grep 1.2.3.4" {
		t.Fatalf("got %q miss %q", got, miss)
	}
	_, miss = Substitute("curl -x socks5h://{user}:{pass}@h", vars)
	if miss != "user" {
		t.Fatalf("missing = %q, want user", miss)
	}
	// {{.Status}} у docker — не переменная
	got, miss = Substitute("docker ps --format '{{.Status}}'", vars)
	if miss != "" || got != "docker ps --format '{{.Status}}'" {
		t.Fatalf("docker format mangled: %q %q", got, miss)
	}
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		expect, out string
		code        int
		want        Status
	}{
		{"rc=0", "", 0, Pass},
		{"rc=0", "", 1, Bad},
		{">5000", "6387", 0, Pass},
		{">5000", "12", 0, Bad},
		{">=2", "2", 0, Pass},
		{"<180", "37", 0, Pass},
		{"<180", "900", 0, Bad},
		{"<180", "", 0, Bad},
		{"nonempty", "x", 0, Pass},
		{"nonempty", "  ", 0, Bad},
		{"active", "active active", 0, Pass},
		{"active", "active failed", 0, Bad},
		{"3.1.", "3.1.20260812", 0, Pass},
		{"0% packet loss", "3 packets transmitted, 3 received, 0% packet loss", 0, Pass},
		{"203.0.113.21", "", 0, Bad},
		{"нет ответа от службы", "что-то", 0, Look},
	}
	for _, c := range cases {
		if got, note := Evaluate(c.expect, c.out, c.code); got != c.want {
			t.Errorf("Evaluate(%q, %q) = %v (%s), want %v", c.expect, c.out, got, note, c.want)
		}
	}
}
