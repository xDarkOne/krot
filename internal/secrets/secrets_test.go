package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.age")
	t.Setenv(PassEnv, "correct horse battery staple")

	s, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("eu/monitoring/token", "p@ss w0rd"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bad-key", "x"); err == nil {
		t.Fatal("ключ без двух / должен отвергаться")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) == "" || contains(raw, "p@ss w0rd") {
		t.Fatal("секрет лежит в файле открытым текстом")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("права %v, ждали 600", fi.Mode().Perm())
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s2.Get("eu/monitoring/token"); v != "p@ss w0rd" {
		t.Fatalf("got %q", v)
	}
	if m := s2.ForModule("eu", "monitoring"); m["token"] != "p@ss w0rd" {
		t.Fatalf("ForModule: %v", m)
	}

	t.Setenv(PassEnv, "wrong password here")
	if _, err := Open(path); err == nil {
		t.Fatal("неверный пароль должен давать ошибку")
	}
}

func TestMask(t *testing.T) {
	got := Mask("curl -x socks5h://u:hunter22@h", []string{"hunter22"})
	if got != "curl -x socks5h://u:<secret>@h" {
		t.Fatal(got)
	}
}

func contains(b []byte, s string) bool {
	return len(s) > 0 && len(b) >= len(s) && (string(b) == s || indexOf(string(b), s) >= 0)
}

func indexOf(a, b string) int {
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i:i+len(b)] == b {
			return i
		}
	}
	return -1
}

func TestKeyMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VPNKIT_HOME", dir)
	t.Setenv(PassEnv, "")
	id, err := CreateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateKey(); err == nil {
		t.Fatal("второй CreateKey должен отказать")
	}
	path := filepath.Join(dir, "secrets.age")
	s, err := CreateWithKey(path, id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Set("ru/x/y", "value-123")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path) // без пароля: ключ из VPNKIT_HOME
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s2.Get("ru/x/y"); v != "value-123" {
		t.Fatalf("got %q", v)
	}
}
