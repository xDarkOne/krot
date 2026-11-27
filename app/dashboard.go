package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/internal/status"
)

//go:embed dashboard_stats.py
var dashboardStats string

type TrafficBucket struct {
	At    int64            `json:"at"`
	Bytes map[string]int64 `json:"bytes"`
}

type ServerStats struct {
	Uptime        string          `json:"uptime"`
	BootedAt      int64           `json:"booted_at"`
	UptimeSeconds int64           `json:"uptime_seconds"`
	Interface     string          `json:"interface"`
	RX            int64           `json:"rx"`
	TX            int64           `json:"tx"`
	Traffic       []TrafficBucket `json:"traffic"`
	HasHistory    bool            `json:"has_history"`
	HistoryError  bool            `json:"history_error"`
}

// ServerStatistics reads counters without running health checks or changing the node.
func (a *App) ServerStatistics(name string, days int) (ServerStats, error) {
	var stats ServerStats
	if days != 1 && days != 7 && days != 30 {
		return stats, fmt.Errorf("неизвестный период статистики")
	}
	n, err := a.node(name)
	if err != nil {
		return stats, err
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return stats, err
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	history := 0
	if n.Role == "entry" || n.Role == "solo" {
		history = 1
	}
	r, err := cl.RunStdin(ctx, fmt.Sprintf("python3 - %d %d", days, history), dashboardStats)
	if err != nil {
		return stats, err
	}
	if r.Code != 0 {
		return stats, fmt.Errorf("не удалось прочитать статистику: %s", strings.TrimSpace(r.Stderr))
	}
	if err := json.Unmarshal([]byte(r.Stdout), &stats); err != nil {
		return stats, fmt.Errorf("не удалось разобрать статистику узла: %w", err)
	}
	stats.Uptime = status.Duration(time.Duration(stats.UptimeSeconds) * time.Second)
	return stats, nil
}

// RestartServer schedules a reboot after SSH has had time to return the result.
func (a *App) RestartServer(name, host string) error {
	a.mu.Lock()
	if a.inv == nil {
		a.mu.Unlock()
		return fmt.Errorf("файл узлов не открыт")
	}
	n, err := a.inv.Node(name)
	if err != nil || n.Host != host {
		a.mu.Unlock()
		return fmt.Errorf("узел изменился. Откройте статистику снова")
	}
	if a.updating || a.busy[name] != "" {
		a.mu.Unlock()
		return fmt.Errorf("дождитесь завершения установки или обновления")
	}
	a.busy[name] = "перезагрузка"
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.busy, name); a.mu.Unlock() }()
	cl, err := remote.Dial(n)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	r, err := cl.Run(ctx, "systemd-run --unit=vpnkit-dashboard-reboot --on-active=5s --timer-property=AccuracySec=1s /usr/bin/systemctl reboot\n")
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return fmt.Errorf("не удалось запланировать перезагрузку: %s", strings.TrimSpace(r.Stderr))
	}
	return nil
}
