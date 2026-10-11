// vpnkit — приложение пульта (Windows, macOS, Linux): состояние узлов и клиенты.
// Логика — пакеты internal/, те же, что у CLI cmd/vpnkit.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/xDarkOne/vpnkit/internal/selfupdate"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	selfupdate.Cleanup(selfupdate.App) // прежняя версия после обновления
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "vpnkit",
		Width:     1180,
		Height:    780,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 245, B: 242, A: 1},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println("Ошибка:", err.Error())
	}
}
