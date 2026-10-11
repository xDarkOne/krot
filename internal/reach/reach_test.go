package reach

import "testing"

func TestIsCloudflare(t *testing.T) {
	for ip, want := range map[string]bool{
		"104.21.3.4": true, "172.67.1.1": true, "188.114.97.3": true,
		"203.0.113.132": false, "198.51.100.246": false, "8.8.8.8": false,
	} {
		if IsCloudflare(ip) != want {
			t.Errorf("%s: ждали %v", ip, want)
		}
	}
}

func TestDNSProblem(t *testing.T) {
	r := DNSResult{Name: "a.example", Want: "1.2.3.4", Answers: []DNSAnswer{{Addrs: []string{"104.21.3.4"}}}}
	if p := dnsProblem(r); !contains(p, "проксируется Cloudflare") {
		t.Errorf("Cloudflare не распознан: %s", p)
	}
	r.Answers = []DNSAnswer{{Addrs: []string{"5.6.7.8"}}}
	if p := dnsProblem(r); contains(p, "Cloudflare") || !contains(p, "исправьте") {
		t.Errorf("чужой адрес: %s", p)
	}
	r.Answers = []DNSAnswer{{Error: "нет записи"}}
	if p := dnsProblem(r); !contains(p, "ещё нет") {
		t.Errorf("нет записи: %s", p)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
