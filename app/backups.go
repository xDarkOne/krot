package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xDarkOne/vpnkit/internal/backup"
	"github.com/xDarkOne/vpnkit/internal/clients"
	"github.com/xDarkOne/vpnkit/internal/remote"
)

// BackupFile — копия узла на этом компьютере.
type BackupFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	At   string `json:"at"`
}

// BackupReport — итог «забрать и проверить».
type BackupReport struct {
	File    BackupFile `json:"file"`
	Files   int        `json:"files"`
	Modules []string   `json:"modules"`
	Missing []string   `json:"missing"`
}

func backupDir(node string) string { return filepath.Join(remote.StateDir(), "backups", node) }

// Backups — копии узла, уже забранные на этот компьютер (новые сверху).
func (a *App) Backups(node string) ([]BackupFile, error) {
	ents, err := os.ReadDir(backupDir(node))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []BackupFile
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz.age") {
			continue
		}
		i, _ := e.Info()
		out = append(out, BackupFile{Path: filepath.Join(backupDir(node), e.Name()), Name: e.Name(),
			Size: i.Size(), At: i.ModTime().Format("02.01.2006 15:04")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// PullBackup — забрать свежую копию узла, сверить sha256 и расшифровать ключом пульта.
func (a *App) PullBackup(node string) (BackupReport, error) {
	n, err := a.node(node)
	if err != nil {
		return BackupReport{}, err
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return BackupReport{}, err
	}
	p, err := backup.Pull(a.ctx, cl, backupDir(n.Name))
	cl.Close()
	if err != nil {
		return BackupReport{}, err
	}
	rep, err := backup.Verify(p, n.Role)
	if err != nil {
		return BackupReport{}, err
	}
	i, _ := os.Stat(p)
	return BackupReport{File: BackupFile{Path: p, Name: filepath.Base(p), Size: i.Size(), At: i.ModTime().Format("02.01.2006 15:04")},
		Files: rep.Files, Modules: rep.Modules, Missing: rep.Missing}, nil
}

// RestoreClients — вернуть клиентов на вход: подписка и Telegram из копии, AWG из хранилища.
// Ход — событиями "log".
func (a *App) RestoreClients(node, path string) error {
	n, err := a.node(node)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(backupDir(n.Name))) {
		return os.ErrPermission // только копии этого узла из папки пульта
	}
	log := func(s string) { runtime.EventsEmit(a.ctx, "log", s) }
	if data, err := backup.ReadFile(path, "etc/vpnkit/users.json"); err == nil {
		if err := clients.RestoreRegistry(a.ctx, n, data, log); err != nil {
			return err
		}
	} else {
		log("в копии нет реестра подписки — пропускаю")
	}
	sec, err := a.vault()
	if err != nil {
		return err
	}
	added, extra, err := clients.Resync(a.ctx, n, "", sec, log)
	if err != nil {
		log("AWG: " + err.Error())
		return nil
	}
	if len(added) == 0 {
		log("AWG: все пиры на месте")
	}
	if len(extra) > 0 {
		log("AWG: на узле есть пиры не из хранилища (не тронуты): " + strings.Join(extra, ", "))
	}
	return nil
}
