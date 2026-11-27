package main

import (
	"strings"
	"testing"

	"github.com/xDarkOne/krot/internal/inventory"
)

func TestServerActionsRejectInvalidOrBusyRequests(t *testing.T) {
	app := &App{
		inv: &inventory.Inventory{Nodes: map[string]*inventory.Node{
			"entry": {Name: "entry", Host: "192.0.2.10", Role: "entry"},
		}},
		busy: map[string]string{"entry": "xray"},
	}

	for _, tc := range []struct {
		name string
		run  func() error
		want string
	}{
		{"statistics rejects an invalid period", func() error {
			_, err := app.ServerStatistics("entry", 2)
			return err
		}, "неизвестный период статистики"},
		{"reboot rejects a stale address", func() error {
			return app.RestartServer("entry", "192.0.2.11")
		}, "узел изменился"},
		{"reboot refuses an active module operation", func() error {
			return app.RestartServer("entry", "192.0.2.10")
		}, "дождитесь завершения"},
		{"forced backup refuses an active module operation", func() error {
			_, err := app.CreateBackup("entry")
			return err
		}, "дождитесь завершения"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
			if got := app.busy["entry"]; got != "xray" {
				t.Fatalf("node operation = %q, want xray", got)
			}
		})
	}
}
