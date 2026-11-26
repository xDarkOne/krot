package modexec

import (
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/secrets"
)

func TestCascadePortReconfiguration(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	store, err := secrets.CreateWithKey(filepath.Join(t.TempDir(), "secrets.age"), id)
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	exit := &inventory.Node{Name: "eu", Role: "exit", Host: "192.0.2.1", Modules: map[string]map[string]any{"cascade-awg0": cfg}}
	entry := &inventory.Node{Name: "ru", Role: "entry"}
	inv := &inventory.Inventory{Nodes: map[string]*inventory.Node{"eu": exit, "ru": entry}}
	prepare := func(n *inventory.Node) map[string]string {
		t.Helper()
		p, err := Prepare(inv, n, "cascade-awg0", store)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	original := prepare(exit)
	cfg["port"] = 443
	updated := prepare(exit)
	client := prepare(entry)
	if updated["PORT"] != "443" || client["ENDPOINT"] != "192.0.2.1:443" {
		t.Fatal("exit and entry did not adopt inventory port")
	}
	for _, k := range []string{"PRIVATE", "PEER_PUBLIC", "PSK", "HPK", "OBFS"} {
		if original[k] != updated[k] {
			t.Fatalf("changing port rotated %s", k)
		}
	}
	delete(cfg, "port")
	if prepare(entry)["PORT"] != "443" {
		t.Fatal("saved port was lost")
	}
	cfg["port"] = 65536
	if _, err := Prepare(inv, exit, "cascade-awg0", store); err == nil {
		t.Fatal("invalid port accepted")
	}
}
