package modexec

import (
	"crypto/ecdh"
	"encoding/base64"
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

func TestCascadeReservesShareSettingsAndCredentials(t *testing.T) {
	t.Setenv("KROT_HOME", t.TempDir())
	id, err := secrets.CreateKey()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "secrets.age")
	store, err := secrets.CreateWithKey(file, id)
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"port": 443, "exit_ipv6": "2001:db8::20",
		"reality_server_name": "tls.example.org", "reality_port": 24443,
	}
	exit := &inventory.Node{Name: "eu", Role: "exit", Host: "192.0.2.1", Modules: map[string]map[string]any{"cascade-awg0": cfg}}
	entry := &inventory.Node{Name: "ru", Role: "entry", Host: "192.0.2.2"}
	inv := &inventory.Inventory{Nodes: map[string]*inventory.Node{"eu": exit, "ru": entry}}
	server, err := Prepare(inv, exit, "cascade-awg0", store)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := secrets.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	client, err := Prepare(inv, entry, "cascade-awg0", reopened)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"REALITY_UUID", "REALITY_SHORT_ID", "REALITY_PORT"} {
		if server[name] == "" || server[name] != client[name] {
			t.Fatalf("exit and entry disagree on %s", name)
		}
	}
	if client["ENDPOINT"] != "192.0.2.1:443" || client["ENDPOINT_V6"] != "[2001:db8::20]:443" {
		t.Fatal("IPv4 must remain the primary endpoint and IPv6 must use the same UDP port")
	}
	raw, err := base64.RawURLEncoding.DecodeString(server["REALITY_PRIVATE"])
	if err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	if client["REALITY_PUBLIC"] != base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()) ||
		client["REALITY_PRIVATE"] != "" || server["REALITY_PUBLIC"] != "" {
		t.Fatal("incorrect distribution of Reality keys")
	}
	delete(cfg, "reality_server_name")
	disabled, err := Prepare(inv, entry, "cascade-awg0", reopened)
	if err != nil {
		t.Fatal(err)
	}
	if disabled["REALITY_NAME"] != "" || disabled["REALITY_PRIVATE"] != "" || disabled["REALITY_PORT"] != "" {
		t.Fatal("removing the Reality hostname must disable the TCP reserve")
	}
}
