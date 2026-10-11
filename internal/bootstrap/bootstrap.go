// Package bootstrap — первый вход на чистый сервер: перевод SSH с пароля на ключ.
//
// Порядок защищает от «запер сам себя»: исходная сессия по паролю держится
// открытой до конца; пароли запрещаются только после того, как вход по ключу
// проверен ОТДЕЛЬНЫМ подключением; после запрета ключ проверяется ещё раз,
// и при неудаче правка откатывается через ту же исходную сессию.
package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/remote"
	"golang.org/x/crypto/ssh"
)

// HardeningPath — drop-in sshd. Номер меньше 50: в образах хостеров лежит
// 50-cloud-init.conf с PasswordAuthentication yes, а sshd берёт первое значение.
const HardeningPath = "/etc/ssh/sshd_config.d/01-vpnkit-hardening.conf"

const hardening = `# vpnkit: вход только по ключу (bootstrap). Номер < 50 — раньше 50-cloud-init.conf.
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
MaxAuthTries 3
`

// EnsureKey возвращает публичный ключ из файла ключа узла, создавая ключ ed25519, если его нет.
func EnsureKey(path string) (ssh.PublicKey, bool, error) {
	path = remote.ExpandHome(path)
	if b, err := os.ReadFile(path); err == nil {
		s, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", path, err)
		}
		return s.PublicKey(), false, nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, err
	}
	blk, err := ssh.MarshalPrivateKey(priv, "vpnkit")
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(blk), 0o600); err != nil {
		return nil, false, err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, false, err
	}
	_ = os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(sp), 0o644)
	return sp, true, nil
}

func run(ctx context.Context, cl *remote.Client, script string) (string, error) {
	r, err := cl.Run(ctx, script)
	out := strings.TrimSpace(r.Stdout + r.Stderr)
	if err != nil {
		return out, err
	}
	if r.Code != 0 {
		return out, fmt.Errorf("код %d: %s", r.Code, out)
	}
	return out, nil
}

// Run переводит узел с пароля на ключ. log получает строки хода работы.
func Run(ctx context.Context, n *inventory.Node, password string, log func(string)) error {
	if n.SSH.Key == "" {
		return errors.New("в inventory не задан ssh.key — куда сохранить ключ пульта для узла")
	}
	pub, created, err := EnsureKey(n.SSH.Key)
	if err != nil {
		return err
	}
	if created {
		log("создан ключ " + remote.ExpandHome(n.SSH.Key))
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " vpnkit"

	// 1. исходная сессия — по паролю (держим до конца)
	pw := []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
		a := make([]string, len(qs))
		for i := range a {
			a[i] = password
		}
		return a, nil
	})}
	c1, err := remote.DialAuth(n, pw)
	if err != nil {
		return fmt.Errorf("вход по паролю: %w", err)
	}
	defer c1.Close()
	if out, err := run(ctx, c1, "id -u\n"); err != nil || out != "0" {
		return fmt.Errorf("вход есть, но не root (id -u = %q): %v", out, err)
	}
	log("вход по паролю: root")

	// 2. ключ в authorized_keys (идемпотентно)
	script := fmt.Sprintf(`set -e
umask 077; mkdir -p /root/.ssh; touch /root/.ssh/authorized_keys
grep -qxF '%[1]s' /root/.ssh/authorized_keys || echo '%[1]s' >> /root/.ssh/authorized_keys
chmod 700 /root/.ssh; chmod 600 /root/.ssh/authorized_keys
echo ok
`, authorized)
	if _, err := run(ctx, c1, script); err != nil {
		return fmt.Errorf("запись authorized_keys: %w", err)
	}
	log("ключ пульта добавлен в /root/.ssh/authorized_keys")

	// 3. вход по ключу — отдельным подключением, ДО запрета паролей
	if err := checkKey(ctx, n); err != nil {
		return fmt.Errorf("вход по ключу не работает, пароли НЕ отключались: %w", err)
	}
	log("вход по ключу проверен отдельным подключением")

	// 4. запрет паролей: sshd -t до применения, reload (сессии не рвутся)
	apply := fmt.Sprintf(`set -e
cat > %[1]s.new <<'EOF'
%[2]sEOF
mv %[1]s.new %[1]s; chmod 644 %[1]s
sshd -t
systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null || systemctl restart ssh
sshd -T | grep -E '^(permitrootlogin|passwordauthentication|kbdinteractiveauthentication) ' | tr '\n' ';'
`, HardeningPath, hardening)
	out, err := run(ctx, c1, apply)
	if err != nil {
		rollback(ctx, c1, log)
		return fmt.Errorf("применение запрета паролей: %w", err)
	}
	if !strings.Contains(out, "passwordauthentication no") || !strings.Contains(out, "permitrootlogin without-password") && !strings.Contains(out, "permitrootlogin prohibit-password") {
		rollback(ctx, c1, log)
		return fmt.Errorf("sshd не принял настройки: %s", out)
	}
	log("sshd: " + out)

	// 5. проверка после: ключ пускает, пароль — нет
	time.Sleep(time.Second)
	if err := checkKey(ctx, n); err != nil {
		rollback(ctx, c1, log)
		return fmt.Errorf("после запрета паролей ключ не пустил — откатил: %w", err)
	}
	if c, err := remote.DialAuth(n, pw); err == nil {
		c.Close()
		// откат вернул бы пароли — оставляем запрет и сообщаем
		return errors.New("после запрета пароль всё ещё пускает: смотрите sshd -T и порядок файлов в sshd_config.d")
	}
	log("готово: вход по ключу работает, пароль отвергается")
	return nil
}

func checkKey(ctx context.Context, n *inventory.Node) error {
	k := *n
	k.SSH.Auth = "key"
	c, err := remote.Dial(&k)
	if err != nil {
		return err
	}
	defer c.Close()
	out, err := run(ctx, c, "id -u\n")
	if err != nil {
		return err
	}
	if out != "0" {
		return fmt.Errorf("id -u = %q", out)
	}
	return nil
}

func rollback(ctx context.Context, c1 *remote.Client, log func(string)) {
	_, err := run(ctx, c1, "rm -f "+HardeningPath+"; sshd -t && (systemctl reload ssh 2>/dev/null || systemctl reload sshd 2>/dev/null || systemctl restart ssh)\n")
	if err != nil {
		log("ОТКАТ НЕ УДАЛСЯ: " + err.Error())
		return
	}
	log("откат: запрет паролей снят, вход как был")
}
