// Package selfupdate — обновление самого krot (окна и CLI) из релизов GitHub.
//
// Откуда брать релизы, задаёт сборка (-ldflags "-X main.updateRepo=владелец/репо"): у сборок
// без этого и у версии «dev» обновлений нет. Файл релиза сверяется по SHA256SUMS того же релиза,
// затем подменяет исполняемый файл (на macOS — весь .app) атомарным переименованием; прежний
// остаётся рядом с суффиксом .old и удаляется при следующем запуске.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Kind — что обновляем: окно или CLI (у них разные файлы в релизе).
type Kind string

const (
	App Kind = "app"
	CLI Kind = "cli"
)

// Release — последний релиз и файл для этой системы.
type Release struct {
	Version string `json:"version"` // без «v»
	Notes   string `json:"notes"`
	URL     string `json:"url"` // страница релиза
	asset   string // имя файла для этой системы
	assetDL string
	sumsDL  string
}

// Check — состояние обновлений для окна.
type Check struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Newer     bool   `json:"newer"`
	CanApply  bool   `json:"canApply"` // можно обновиться на месте (иначе — ссылка на страницу релиза)
	Reason    string `json:"reason"`   // почему нельзя на месте или почему проверки нет
	URL       string `json:"url"`
	Notes     string `json:"notes"`
	CheckedAt string `json:"checkedAt"`
}

var client = &http.Client{Timeout: 5 * time.Minute}

// osName — имя системы в именах файлов релиза.
func osName() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// assetName — файл релиза для этой системы.
func assetName(k Kind, v string) string {
	switch {
	case k == CLI && runtime.GOOS == "windows":
		return fmt.Sprintf("krot-cli-%s-windows-%s.exe", v, runtime.GOARCH)
	case k == CLI:
		return fmt.Sprintf("krot-cli-%s-%s-%s", v, osName(), runtime.GOARCH)
	case runtime.GOOS == "windows":
		return fmt.Sprintf("krot-app-%s-windows-%s.exe", v, runtime.GOARCH)
	case runtime.GOOS == "darwin":
		return fmt.Sprintf("krot-app-%s-macos-universal.zip", v)
	default:
		return fmt.Sprintf("krot-app-%s-linux-%s.tar.gz", v, runtime.GOARCH)
	}
}

// Latest — последний (не пред-) релиз репозитория.
func Latest(ctx context.Context, repo string, k Kind) (*Release, error) {
	if repo == "" {
		return nil, errors.New("сборка без адреса релизов — обновлять неоткуда")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	auth(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub недоступен: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub ответил %s", resp.Status)
	}
	var r struct {
		Tag    string `json:"tag_name"`
		Body   string `json:"body"`
		URL    string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			API  string `json:"url"` // через API: работает и для приватных репозиториев
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	rel := &Release{Version: strings.TrimPrefix(r.Tag, "v"), Notes: r.Body, URL: r.URL}
	rel.asset = assetName(k, rel.Version)
	for _, a := range r.Assets {
		switch a.Name {
		case rel.asset:
			rel.assetDL = a.API
		case "SHA256SUMS":
			rel.sumsDL = a.API
		}
	}
	return rel, nil
}

// Newer — новее ли версия b, чем a (числа через точку; суффикс после «-» — пред-версия, она старше).
func Newer(a, b string) bool {
	pa, sa := split(a)
	pb, sb := split(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pb[i] > pa[i]
		}
	}
	return sa != "" && sb == "" // 0.2.0-test < 0.2.0
}

func split(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	suf := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, suf = v[:i], v[i+1:]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out, suf
}

// Status — есть ли обновление и можно ли поставить его на месте.
func Status(ctx context.Context, repo, current string, k Kind) Check {
	c := Check{Current: current, CheckedAt: time.Now().Format(time.RFC3339)}
	if current == "" || strings.Contains(current, "dev") {
		c.Reason = "сборка для разработки — обновления выключены"
		return c
	}
	rel, err := Latest(ctx, repo, k)
	if err != nil {
		c.Reason = err.Error()
		return c
	}
	c.Latest, c.URL, c.Notes = rel.Version, rel.URL, rel.Notes
	c.Newer = Newer(current, rel.Version)
	if !c.Newer {
		return c
	}
	if rel.assetDL == "" || rel.sumsDL == "" {
		c.Reason = "в релизе нет файла для этой системы"
		return c
	}
	if _, err := target(k); err != nil {
		c.Reason = err.Error()
		return c
	}
	c.CanApply = true
	return c
}

// target — что подменять: исполняемый файл, а на macOS для окна — весь .app.
func target(k Kind) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	t := exe
	if k == App && runtime.GOOS == "darwin" {
		t = filepath.Dir(filepath.Dir(filepath.Dir(exe))) // …/krot.app/Contents/MacOS/vpnkit
		if !strings.HasSuffix(t, ".app") {
			return "", errors.New("приложение запущено не из .app — обновите вручную")
		}
	}
	// писать нужно в каталог рядом: новый файл кладётся туда же и переименовывается поверх
	probe, err := os.CreateTemp(filepath.Dir(t), ".vpnkit-update-*")
	if err != nil {
		return "", fmt.Errorf("нет прав на запись в %s — обновите вручную со страницы релиза", filepath.Dir(t))
	}
	probe.Close()
	os.Remove(probe.Name())
	return t, nil
}

// Apply скачивает, сверяет и ставит новую версию. Возвращает путь к новому исполняемому
// файлу (или .app) — его запускает вызывающий, после чего текущий процесс завершается.
func Apply(ctx context.Context, repo string, k Kind, log func(string)) (string, error) {
	rel, err := Latest(ctx, repo, k)
	if err != nil {
		return "", err
	}
	if rel.assetDL == "" || rel.sumsDL == "" {
		return "", errors.New("в релизе нет файла для этой системы")
	}
	t, err := target(k)
	if err != nil {
		return "", err
	}
	log("скачиваю " + rel.asset)
	data, err := get(ctx, rel.assetDL)
	if err != nil {
		return "", err
	}
	sums, err := get(ctx, rel.sumsDL)
	if err != nil {
		return "", err
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == rel.asset {
			want = f[0]
		}
	}
	got := sha256.Sum256(data)
	if want == "" || hex.EncodeToString(got[:]) != want {
		return "", errors.New("контрольная сумма не совпала с SHA256SUMS — обновление не поставлено")
	}
	log("контрольная сумма совпала, ставлю " + rel.Version)
	dir := filepath.Dir(t)
	switch {
	case k == App && runtime.GOOS == "darwin":
		return replaceApp(data, t)
	case k == App && runtime.GOOS == "linux":
		bin, err := fromTarGz(data, "krot-app")
		if err != nil {
			return "", err
		}
		return t, replaceFile(bin, t, dir)
	default:
		return t, replaceFile(data, t, dir)
	}
}

// auth — GITHUB_TOKEN (необязательно): приватный репозиторий и лимит запросов повыше.
func auth(req *http.Request) {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
}

func get(ctx context.Context, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	auth(req)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

func fromTarGz(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("в архиве нет %s", name)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == name {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}

// replaceFile — новый файл рядом, текущий → .old, новый → на место. Работает и для
// запущенного exe на Windows: переименовать его можно, перезаписать — нет.
func replaceFile(data []byte, t, dir string) error {
	tmp, err := os.CreateTemp(dir, ".vpnkit-new-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	os.Chmod(tmp.Name(), 0o755)
	old := t + ".old"
	os.Remove(old)
	if err := os.Rename(t, old); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), t); err != nil {
		os.Rename(old, t) // вернуть как было
		return err
	}
	return nil
}

// replaceApp — распаковать .app из zip рядом с текущим и поменять местами каталоги.
func replaceApp(data []byte, app string) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(app), ".vpnkit-new-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	for _, f := range zr.File {
		p := filepath.Join(stage, f.Name)
		if !strings.HasPrefix(p, stage+string(os.PathSeparator)) {
			return "", fmt.Errorf("подозрительный путь в архиве: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(p, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(p), 0o755)
		if f.Mode()&os.ModeSymlink != 0 {
			r, _ := f.Open()
			dst, _ := io.ReadAll(r)
			r.Close()
			if err := os.Symlink(string(dst), p); err != nil {
				return "", err
			}
			continue
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		w, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode().Perm())
		if err != nil {
			r.Close()
			return "", err
		}
		_, err = io.Copy(w, r)
		r.Close()
		w.Close()
		if err != nil {
			return "", err
		}
	}
	matches, _ := filepath.Glob(filepath.Join(stage, "*.app"))
	if len(matches) != 1 {
		return "", errors.New("в архиве нет .app")
	}
	old := app + ".old"
	os.RemoveAll(old)
	if err := os.Rename(app, old); err != nil {
		return "", err
	}
	if err := os.Rename(matches[0], app); err != nil {
		os.Rename(old, app)
		return "", err
	}
	return app, nil
}

// Cleanup — убрать то, что осталось от прошлого обновления (вызывать при запуске).
func Cleanup(k Kind) {
	t, err := os.Executable()
	if err != nil {
		return
	}
	if t, err = filepath.EvalSymlinks(t); err != nil {
		return
	}
	if k == App && runtime.GOOS == "darwin" {
		t = filepath.Dir(filepath.Dir(filepath.Dir(t)))
	}
	os.RemoveAll(t + ".old")
}
