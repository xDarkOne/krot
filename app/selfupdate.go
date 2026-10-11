package main

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xDarkOne/vpnkit/internal/selfupdate"
)

// Версию и адрес релизов задаёт сборка релиза:
// -ldflags "-X main.version=0.2.0 -X main.updateRepo=владелец/репо". У сборки без них
// (разработка) обновлений нет.
var (
	version    = "dev"
	updateRepo string
)

// Version — версия приложения (для шапки окна).
func (a *App) Version() string { return version }

// CheckUpdate — есть ли новая версия приложения в релизах.
func (a *App) CheckUpdate() selfupdate.Check {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return selfupdate.Status(ctx, updateRepo, version, selfupdate.App)
}

// ApplyUpdate скачивает новую версию, сверяет её с SHA256SUMS релиза, ставит на место
// текущей, запускает и закрывает это окно. Ход — событиями "update-log".
func (a *App) ApplyUpdate() error {
	log := func(s string) { wruntime.EventsEmit(a.ctx, "update-log", s) }
	path, err := selfupdate.Apply(a.ctx, updateRepo, selfupdate.App, log)
	if err != nil {
		return err
	}
	log("перезапускаю")
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-n", path)
	} else {
		cmd = exec.Command(path, os.Args[1:]...)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	wruntime.Quit(a.ctx)
	return nil
}
