// Package remote — SSH-сессии к узлам: ключ или пароль, свой known_hosts
// с закреплением ключа при первом подключении.
package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/xDarkOne/krot/internal/env"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xDarkOne/krot/internal/inventory"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

// Client — подключение к одному узлу.
type Client struct {
	node *inventory.Node
	c    *ssh.Client
	jump *ssh.Client // промежуточный узел, если есть
}

// Result — итог команды.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// StateDir — каталог пульта (~/.krot).
func StateDir() string { return env.Home() }

// Dial подключается к узлу способом из inventory (ключ или пароль).
func Dial(n *inventory.Node) (*Client, error) {
	auth, err := authMethods(n)
	if err != nil {
		return nil, err
	}
	return DialAuth(n, auth)
}

// preferredHostKeyAlgos — порядок для узла, ключ которого ещё не закреплён: Ed25519 первым.
var preferredHostKeyAlgos = []string{
	ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
}

// hostKeyAlgos — какие типы ключа просить у узла. Если ключ уже закреплён, просим
// ИМЕННО его тип: иначе после обновления OpenSSH сервер может первым предложить
// ключ другого типа, и Go-клиент сочтёт это подменой (поймано 02.10.2026 на 24.04).
func hostKeyAlgos(addr string) []string {
	b, err := os.ReadFile(filepath.Join(StateDir(), "known_hosts"))
	if err != nil {
		return preferredHostKeyAlgos
	}
	host := knownhosts.Normalize(addr)
	seen := map[string]bool{}
	var out []string
	add := func(a ...string) {
		for _, x := range a {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		match := false
		for _, h := range strings.Split(f[0], ",") {
			if h == host {
				match = true
			}
		}
		if !match {
			continue
		}
		if f[1] == ssh.KeyAlgoRSA {
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		} else {
			add(f[1])
		}
	}
	if len(out) == 0 {
		return preferredHostKeyAlgos
	}
	return out
}

// DialAuth подключается к узлу с явно заданными способами входа — например,
// паролем при первом входе (bootstrap). Учитывает промежуточный узел ssh.jump.
func DialAuth(n *inventory.Node, auth []ssh.AuthMethod) (*Client, error) {
	hk, err := hostKeyCallback()
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(n.Host, strconv.Itoa(n.SSH.Port))
	cfg := &ssh.ClientConfig{User: n.SSH.User, Auth: auth, HostKeyCallback: hk, Timeout: 15 * time.Second,
		HostKeyAlgorithms: hostKeyAlgos(addr)}
	if n.SSH.Jump == nil {
		c, err := ssh.Dial("tcp", addr, cfg)
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", n.Name, addr, err)
		}
		return &Client{node: n, c: c}, nil
	}
	j := n.SSH.Jump
	signer, err := loadKey(n.Name+" (jump)", j.Key)
	if err != nil {
		return nil, err
	}
	jaddr := net.JoinHostPort(j.Host, strconv.Itoa(j.Port))
	jc, err := ssh.Dial("tcp", jaddr, &ssh.ClientConfig{User: j.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hk, Timeout: 15 * time.Second, HostKeyAlgorithms: hostKeyAlgos(jaddr)})
	if err != nil {
		return nil, fmt.Errorf("%s: промежуточный узел %s: %w", n.Name, jaddr, err)
	}
	conn, err := jc.Dial("tcp", addr)
	if err != nil {
		jc.Close()
		return nil, fmt.Errorf("%s: через %s до %s: %w", n.Name, jaddr, addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		jc.Close()
		return nil, fmt.Errorf("%s (%s через %s): %w", n.Name, addr, jaddr, err)
	}
	return &Client{node: n, c: ssh.NewClient(cc, chans, reqs), jump: jc}, nil
}

// Close закрывает подключение.
func (cl *Client) Close() error {
	err := cl.c.Close()
	if cl.jump != nil {
		cl.jump.Close()
	}
	return err
}

// Run выполняет скрипт через stdin оболочки узла (sh -s), чтобы не зависеть
// от экранирования и работать и в bash, и в busybox ash.
func (cl *Client) Run(ctx context.Context, script string) (Result, error) {
	return cl.RunStdin(ctx, "sh -s", script)
}

// RunStdin выполняет одну команду, подавая ей stdin. Так секреты (токены) попадают в
// программу мимо её аргументов, которые на узле видны всем через ps (P-50).
func (cl *Client) RunStdin(ctx context.Context, cmd, stdin string) (Result, error) {
	s, err := cl.c.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer s.Close()
	var out, errb bytes.Buffer
	s.Stdout, s.Stderr = &out, &errb
	s.Stdin = strings.NewReader(stdin)
	done := make(chan error, 1)
	go func() { done <- s.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		return Result{Stdout: out.String(), Stderr: errb.String(), Code: -1}, ctx.Err()
	case err := <-done:
		code := 0
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			code, err = ee.ExitStatus(), nil
		}
		return Result{Stdout: out.String(), Stderr: errb.String(), Code: code}, err
	}
}

func authMethods(n *inventory.Node) ([]ssh.AuthMethod, error) {
	switch n.SSH.Auth {
	case "key":
		signer, err := loadKey(n.Name, n.SSH.Key)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case "password":
		pw := ""
		if n.SSH.PasswordEnv != "" {
			pw = os.Getenv(n.SSH.PasswordEnv)
		}
		if pw == "" {
			var err error
			pw, err = prompt(fmt.Sprintf("Пароль %s@%s: ", n.SSH.User, n.Host))
			if err != nil {
				return nil, err
			}
		}
		// dropbear на OpenWrt может предлагать keyboard-interactive вместо password
		kbd := ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
			a := make([]string, len(qs))
			for i := range a {
				a[i] = pw
			}
			return a, nil
		})
		return []ssh.AuthMethod{ssh.Password(pw), kbd}, nil
	}
	return nil, fmt.Errorf("узел %s: неизвестный способ входа %q (key|password)", n.Name, n.SSH.Auth)
}

// loadKey читает закрытый ключ (по умолчанию ~/.ssh/id_ed25519; ~ раскрывается).
func loadKey(who, path string) (ssh.Signer, error) {
	path = ExpandHome(path)
	if path == "" {
		h, _ := os.UserHomeDir()
		path = filepath.Join(h, ".ssh", "id_ed25519")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("узел %s: ключ %s: %w", who, path, err)
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		var pp *ssh.PassphraseMissingError
		if errors.As(err, &pp) {
			pw, perr := prompt(fmt.Sprintf("Пароль ключа %s: ", path))
			if perr != nil {
				return nil, perr
			}
			signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(pw))
		}
		if err != nil {
			return nil, fmt.Errorf("узел %s: ключ %s: %w", who, path, err)
		}
	}
	return signer, nil
}

// ExpandHome раскрывает ~ в начале пути.
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, path[1:])
	}
	return path
}

// Prompt, если задан, заменяет ввод пароля в терминале (приложение спрашивает в окне).
var Prompt func(question string) (string, error)

func prompt(q string) (string, error) {
	if Prompt != nil {
		return Prompt(q)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("%sнет терминала для ввода — задайте переменную окружения (ssh.password_env)", q)
	}
	fmt.Fprint(os.Stderr, q)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// hostKeyCallback: проверка по ~/.krot/known_hosts; неизвестный ключ
// закрепляется при первом подключении (как StrictHostKeyChecking=accept-new),
// изменившийся — отказ.
func hostKeyCallback() (ssh.HostKeyCallback, error) {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "known_hosts")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return nil, err
		}
	}
	return func(host string, remote net.Addr, key ssh.PublicKey) error {
		cb, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		err = cb(host, remote, key)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			line := knownhosts.Line([]string{knownhosts.Normalize(host)}, key)
			f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			if _, ferr = fmt.Fprintln(f, line); ferr != nil {
				return ferr
			}
			fmt.Fprintf(os.Stderr, "  ключ узла %s закреплён: %s\n", host, ssh.FingerprintSHA256(key))
			return nil
		}
		if errors.As(err, &ke) {
			return fmt.Errorf("ключ узла %s ИЗМЕНИЛСЯ (%s) — возможен перехват; если узел переустановлен, удалите строку из %s", host, ssh.FingerprintSHA256(key), path)
		}
		return err
	}, nil
}

// Fetch скачивает файл с узла побайтно (через cat — sftp не нужен)
// и возвращает sha256 полученного.
func (cl *Client) Fetch(ctx context.Context, path string, w io.Writer) (string, error) {
	s, err := cl.c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	h := sha256.New()
	s.Stdout = io.MultiWriter(w, h)
	var errb bytes.Buffer
	s.Stderr = &errb
	done := make(chan error, 1)
	go func() { done <- s.Run("cat '" + strings.ReplaceAll(path, "'", "") + "'") }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("%s: %v %s", path, err, strings.TrimSpace(errb.String()))
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
