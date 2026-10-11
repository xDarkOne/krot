// Package env — переменные окружения и каталог данных пульта. Программа раньше называлась
// vpnkit: старые имена (VPNKIT_*, ~/.vpnkit) по-прежнему понимаются.
package env

import (
	"os"
	"path/filepath"
)

// Get — KROT_<name>, а если не задана — прежняя VPNKIT_<name>.
func Get(name string) string {
	if v := os.Getenv("KROT_" + name); v != "" {
		return v
	}
	return os.Getenv("VPNKIT_" + name)
}

// Home — каталог пульта: $KROT_HOME, иначе ~/.krot; если его ещё нет, а есть прежний
// ~/.vpnkit — он (ключ пульта, хранилища и known_hosts остаются на месте).
func Home() string {
	if d := Get("HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	krot, old := filepath.Join(h, ".krot"), filepath.Join(h, ".vpnkit")
	if _, err := os.Stat(krot); err != nil {
		if _, err := os.Stat(old); err == nil {
			return old
		}
	}
	return krot
}
