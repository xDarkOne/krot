package remote

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestHostKeyAlgos(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VPNKIT_HOME", dir)
	kh := "198.51.100.246 ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC\n" +
		"[10.0.0.5]:2222 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI\n"
	if err := os.WriteFile(filepath.Join(dir, "known_hosts"), []byte(kh), 0o600); err != nil {
		t.Fatal(err)
	}
	// закреплён RSA → просим RSA (SHA-2 и старый ssh-rsa), а не ECDSA, который сервер может предложить первым
	got := hostKeyAlgos("198.51.100.246:22")
	if len(got) != 3 || got[0] != ssh.KeyAlgoRSASHA512 || got[2] != ssh.KeyAlgoRSA {
		t.Fatalf("rsa: %v", got)
	}
	// нестандартный порт хранится как [host]:port
	if got := hostKeyAlgos("10.0.0.5:2222"); len(got) != 1 || got[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("ed25519: %v", got)
	}
	// новый узел → Ed25519 первым
	if got := hostKeyAlgos("1.2.3.4:22"); got[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("новый: %v", got)
	}
}
