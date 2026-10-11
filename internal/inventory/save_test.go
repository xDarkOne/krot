package inventory

import (
	"path/filepath"
	"testing"
)

func TestNewCascadeSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "inventory.yaml")
	inv := NewCascade("ru", "1.2.3.4", "eu", "5.6.7.8", "/keys", []string{"ru.example.org", "es.example.org"}, nil)
	if err := inv.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	e, err := got.Node("ru")
	if err != nil || e.Role != "entry" || e.Host != "1.2.3.4" || e.SSH.Auth != "key" || e.SSH.Port != 22 {
		t.Fatalf("вход прочитан не так: %+v %v", e, err)
	}
	names, _ := e.Modules["nginx-tls"]["names"].([]any)
	if len(names) != 2 || names[1] != "es.example.org" {
		t.Errorf("имена: %v", e.Modules["nginx-tls"])
	}
	x, _ := got.Node("eu")
	if x.Role != "exit" || len(x.Modules) != 0 || got.Topology != "cascade" {
		t.Errorf("выход: %+v", x)
	}
}
