package modexec

import (
	"context"

	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/secrets"
)

// Artifact — двоичный файл для узла (пакет), скачанный и проверенный пультом.
type Artifact struct {
	Name   string
	Data   []byte
	SHA256 string
}

// artifacts — что пульт должен передать на узел до шага install: файлы, которые узел сам
// скачать не может (российский вход часто не видит GitHub). Сейчас таких модулей нет —
// после ухода роутеров (11.10.2026) механизм оставлен для серверных модулей.
func artifacts(ctx context.Context, n *inventory.Node, module string, params map[string]string, sec *secrets.Store) ([]Artifact, error) {
	return nil, nil
}
