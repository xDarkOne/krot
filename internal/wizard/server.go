package wizard

import (
	"context"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/probe"
	"github.com/xDarkOne/vpnkit/internal/remote"
)

// Finding — находка проверки сервера для экрана.
type Finding struct {
	Level string `json:"level"` // ok | warn | fail
	What  string `json:"what"`
}

// ServerCheck — подходит ли новый сервер под роль.
type ServerCheck struct {
	Findings []Finding `json:"findings"`
	Worst    string    `json:"worst"` // ok | warn | fail
	Error    string    `json:"error"` // не удалось подключиться или собрать факты
}

var levelName = map[probe.Level]string{probe.OK: "ok", probe.Warn: "warn", probe.Fail: "fail"}

// PasswordAuth — вход по паролю root (и keyboard-interactive: так спрашивают некоторые образы).
func PasswordAuth(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
		a := make([]string, len(qs))
		for i := range a {
			a[i] = password
		}
		return a, nil
	})}
}

// CheckServer подключается к ещё не настроенному серверу по паролю root (через n.SSH.Jump, если задан)
// и проверяет его под роль n.Role: ОС, виртуализация, ресурсы, занятые порты. Ничего не меняет.
func CheckServer(ctx context.Context, n *inventory.Node, password string) ServerCheck {
	cl, err := remote.DialAuth(n, PasswordAuth(password))
	if err != nil {
		msg := "не удалось войти по паролю: " + err.Error()
		if strings.Contains(err.Error(), "unable to authenticate") {
			msg = "сервер не принял пароль root: пароль неверный или вход по паролю на нём уже отключён " +
				"(сервер настраивали раньше — тогда он уже на ключе)"
		}
		return ServerCheck{Worst: "fail", Error: msg}
	}
	defer cl.Close()
	f, err := probe.Collect(ctx, cl)
	if err != nil {
		return ServerCheck{Worst: "fail", Error: "сбор сведений о сервере: " + err.Error()}
	}
	v := probe.ServerVerdict(f, n.Role)
	out := ServerCheck{Worst: levelName[v.Worst()]}
	for _, x := range v.Findings {
		out.Findings = append(out.Findings, Finding{Level: levelName[x.Level], What: x.What})
	}
	return out
}
