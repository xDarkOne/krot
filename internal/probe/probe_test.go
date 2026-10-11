package probe

import "testing"

// Факты типичного выхода (сокращённо).
var exitNode = Facts{
	"os": "ubuntu", "os_version": "26.04", "arch": "x86_64", "virt": "kvm",
	"mem_mb": "955", "swap_mb": "2047", "disk_free_mb": "7092",
	"awg_module": "3.1.20260812", "public_ip": "203.0.113.21", "if_addrs": "203.0.113.21 10.100.0.1",
	"containers": "someapp-web-1", "p443": "nginx",
}

func has(v Verdict, l Level) bool {
	for _, f := range v.Findings {
		if f.Level == l {
			return true
		}
	}
	return false
}

func TestServerVerdictProd(t *testing.T) {
	v := ServerVerdict(exitNode, "exit")
	if v.Worst() == Fail {
		t.Fatalf("выход не должен проваливаться: %+v", v.Findings)
	}
	if !has(v, Warn) {
		t.Fatal("чужой контейнер должен дать замечание")
	}
}

func TestServerVerdictOpenVZ(t *testing.T) {
	f := Facts{}
	for k, v := range exitNode {
		f[k] = v
	}
	f["virt"] = "openvz"
	f["awg_module"] = ""
	f["headers"] = "(none)"
	if ServerVerdict(f, "exit").Worst() != Fail {
		t.Fatal("OpenVZ без заголовков ядра должен быть НЕ ПОДХОДИТ")
	}
}

func TestServerVerdictNAT(t *testing.T) {
	f := Facts{}
	for k, v := range exitNode {
		f[k] = v
	}
	f["if_addrs"] = "10.0.0.5"
	v := ServerVerdict(f, "exit")
	if v.Worst() != Warn {
		t.Fatalf("сервер за NAT — замечание, а не провал: %+v", v.Findings)
	}
}
