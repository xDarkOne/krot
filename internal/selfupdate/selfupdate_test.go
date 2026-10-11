package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.1.1", true},
		{"0.1.1", "0.1.1", false},
		{"0.1.2", "0.1.1", false},
		{"0.9.0", "0.10.0", true},
		{"v0.1.0", "0.2.0", true},
		{"0.2.0-test", "0.2.0", true},
		{"0.2.0", "0.2.0-test", false},
		{"1.0.0", "0.99.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "krot")
	os.WriteFile(p, []byte("old"), 0o755)
	if err := replaceFile([]byte("new"), p, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Errorf("на месте: %q", b)
	}
	if b, _ := os.ReadFile(p + ".old"); string(b) != "old" {
		t.Errorf("прежний: %q", b)
	}
	if st, _ := os.Stat(p); st.Mode().Perm()&0o100 == 0 {
		t.Error("новый файл не исполняемый")
	}
}

func TestDevBuildNoUpdates(t *testing.T) {
	if c := Status(t.Context(), "x/y", "0.1.0-dev", CLI); c.Newer || c.CanApply || c.Reason == "" {
		t.Errorf("сборка dev: %+v", c)
	}
}
