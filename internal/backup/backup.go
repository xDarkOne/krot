// Package backup — работа пульта с резервными копиями узлов: забрать с узла,
// расшифровать ключом пульта и проверить содержимое, распаковать для восстановления.
// Копии создаёт на узлах vpnkit-backup.sh (модуль backup), шифруя открытым ключом пульта.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/internal/secrets"
)

// ByModule — что обязано лежать в копии узла, поставленного установщиком: по меткам
// var/lib/vpnkit/modules/<модуль> в самой копии. Шаблоны — как в path.Match.
var ByModule = map[string][]string{
	"base-host":     {"etc/nftables.conf", "etc/nftables.d/10-vpnkit-fw.nft", "etc/sysctl.d/99-vpnkit.conf"},
	"amneziawg":     {},
	"cascade-awg0":  {"etc/amnezia/amneziawg/awg0.conf", "etc/nftables.d/20-vpnkit-cascade.nft"},
	"split-routing": {"etc/nftables.d/25-vpnkit-split.nft", "etc/dnsmasq.d/10-vpnkit-split.conf", "usr/local/sbin/vpnkit-ru-nets.sh"},
	"client-awg":    {"etc/nftables.d/30-krot-client-*.nft", "etc/dnsmasq.d/30-vpnkit-*.conf"},
	"backup":        {"etc/vpnkit/backup.conf", "etc/vpnkit/backup.recipients", "usr/local/sbin/vpnkit-backup.sh"},
	"monitoring":    {"etc/vpnkit/watch.conf", "usr/local/sbin/vpnkit-watch.sh"},
	"nginx-tls":     {"etc/nginx/sites-available/vpnkit-*", "etc/letsencrypt/renewal/*.conf", "etc/nftables.d/40-vpnkit-nginx.nft"},
	// users.json — ключи всех клиентов подписки: без него восстановление = переиздание всем
	"xray":          {"etc/vpnkit/users.json", "etc/vpnkit/xray.env", "usr/local/sbin/vpnkit-gen.py", "etc/systemd/system/xray.service"},
	"hysteria2":     {"etc/vpnkit/hy2.env", "etc/systemd/system/sing-box.service.d/10-vpnkit-single-config.conf"},
	"subscriptions": {"etc/vpnkit/sub.env"},
	"mtproxy":       {"etc/vpnkit/mtproxy.env", "etc/vpnkit/users.json", "etc/systemd/system/telemt.service"},
}

const markerDir = "var/lib/vpnkit/modules/"

// Pull забирает самую свежую собственную копию узла в dir; возвращает путь.
func Pull(ctx context.Context, cl *remote.Client, dir string) (string, error) {
	r, err := cl.Run(ctx, "ls -1t /var/backups/vpnkit/*.tar.gz.age 2>/dev/null | head -1\n")
	if err != nil {
		return "", err
	}
	src := strings.TrimSpace(r.Stdout)
	if src == "" {
		return "", errors.New("на узле нет копий в /var/backups/vpnkit")
	}
	sum, err := cl.Run(ctx, "cat "+strings.TrimSuffix(src, ".tar.gz.age")+".sha256\n")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(src))
	f, err := os.OpenFile(dst+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	got, err := cl.Fetch(ctx, src, f)
	f.Close()
	if err != nil {
		return "", err
	}
	if want := strings.TrimSpace(sum.Stdout); want != got {
		return "", fmt.Errorf("sha256 не совпал: на узле %s, получено %s", want, got)
	}
	return dst, os.Rename(dst+".part", dst)
}

// Report — итог проверки копии.
type Report struct {
	Files   int
	Bytes   int64
	Missing []string
	Top     map[string]int // число файлов по верхним каталогам
	Modules []string       // модули установщика, найденные в копии (пусто — узел без меток установщика: по роли)
}

// Verify расшифровывает копию ключом пульта и проверяет содержимое (без распаковки на диск).
func Verify(path, role string) (Report, error) {
	rep := Report{Top: map[string]int{}}
	err := walk(path, func(h *tar.Header, _ io.Reader) error {
		if h.Typeflag == tar.TypeReg {
			rep.Files++
			rep.Bytes += h.Size
		}
		name := strings.TrimPrefix(h.Name, "./")
		top := name
		if parts := strings.SplitN(name, "/", 3); len(parts) >= 2 {
			top = parts[0] + "/" + parts[1]
		}
		rep.Top[top]++
		return nil
	})
	if err != nil {
		return rep, err
	}
	have := map[string]bool{}
	var mods []string
	_ = walk(path, func(h *tar.Header, _ io.Reader) error {
		name := strings.TrimPrefix(h.Name, "./")
		have[name] = true
		if h.Typeflag == tar.TypeReg && strings.HasPrefix(name, markerDir) {
			mods = append(mods, strings.TrimPrefix(name, markerDir))
		}
		return nil
	})
	var want []string
	if len(mods) > 0 { // узел ставил установщик — требования по его модулям
		sort.Strings(mods)
		rep.Modules = mods
		for _, m := range mods {
			want = append(want, ByModule[m]...)
		}
	} else { // в копии нет меток установщика — значит, это не копия узла krot
		rep.Missing = append(rep.Missing, markerDir+"* (метки модулей krot)")
	}
	for _, f := range want {
		ok := have[f]
		if !ok && strings.ContainsAny(f, "*?[") {
			for n := range have {
				if m, _ := pathpkg.Match(f, n); m {
					ok = true
					break
				}
			}
		}
		if !ok {
			rep.Missing = append(rep.Missing, f)
		}
	}
	sort.Strings(rep.Missing)
	return rep, nil
}

// Extract распаковывает копию в каталог (для восстановления или разбора).
func Extract(path, dir string) (int, error) {
	n := 0
	err := walk(path, func(h *tar.Header, r io.Reader) error {
		target := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(h.Name, "./")))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("небезопасный путь в архиве: %s", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(target, 0o700)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, r)
			f.Close()
			n++
			return err
		}
		return nil // ссылки и прочее на Windows не восстанавливаем — они в списке tar
	})
	return n, err
}

func walk(path string, fn func(*tar.Header, io.Reader) error) error {
	id, err := secrets.LoadKey()
	if err != nil {
		return err
	}
	if id == nil {
		return fmt.Errorf("нет ключа пульта %s — без него копию не расшифровать", secrets.KeyPath())
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := age.Decrypt(f, id)
	if err != nil {
		return fmt.Errorf("расшифровка: %w", err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// ReadFile достаёт один файл из копии (путь как в архиве, без ведущего «/»).
func ReadFile(path, name string) ([]byte, error) {
	var out []byte
	found := false
	err := walk(path, func(h *tar.Header, r io.Reader) error {
		if found || h.Typeflag != tar.TypeReg || strings.TrimPrefix(h.Name, "./") != name {
			return nil
		}
		b, err := io.ReadAll(io.LimitReader(r, 64<<20))
		out, found = b, true
		return err
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("в копии нет %s", name)
	}
	return out, nil
}
