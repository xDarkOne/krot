package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/remote"
)

// SubCard — клиент подписки: персональная ссылка на подписку и ссылки на входы.
type SubCard struct {
	Name   string   `json:"name"`
	SubURL string   `json:"sub_url"`
	Links  []string `json:"links"`
	// Routing — ссылки на правила маршрутизации для приложений: "happ", "incy" → https://…/h, /i
	Routing map[string]string `json:"routing"`
}

var subName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// gen запускает vpnkit-gen.py на узле. Реестр клиентов подписки живёт на узле
// (/etc/vpnkit/users.json) и попадает в его резервные копии.
func gen(ctx context.Context, n *inventory.Node, args ...string) (string, error) {
	cl, err := remote.Dial(n)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	r, err := cl.Run(ctx, "/usr/local/sbin/vpnkit-gen.py "+strings.Join(args, " ")+"\n")
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		msg := strings.TrimSpace(r.Stderr + r.Stdout)
		if strings.Contains(msg, "No such file") || r.Code == 127 {
			msg = "на узле нет модуля xray (vpnkit-gen.py)"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return r.Stdout, nil
}

func subCard(out string) (SubCard, error) {
	var c SubCard
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &c); err != nil {
		return c, fmt.Errorf("ответ vpnkit-gen.py не разобрать: %w", err)
	}
	return c, nil
}

// SubAdd заводит клиента подписки; службы узла пересобираются там же.
func SubAdd(ctx context.Context, n *inventory.Node, name string) (SubCard, error) {
	if !subName.MatchString(name) {
		return SubCard{}, fmt.Errorf("имя клиента: латиница, цифры, . _ -, до 32 знаков")
	}
	out, err := gen(ctx, n, "add", name)
	if err != nil {
		return SubCard{}, err
	}
	return subCard(out)
}

// SubShow — карточка существующего клиента.
func SubShow(ctx context.Context, n *inventory.Node, name string) (SubCard, error) {
	if !subName.MatchString(name) {
		return SubCard{}, fmt.Errorf("недопустимое имя %q", name)
	}
	out, err := gen(ctx, n, "show", name)
	if err != nil {
		return SubCard{}, err
	}
	return subCard(out)
}

// SubDel удаляет клиента: его подписка и ключи перестают работать сразу.
func SubDel(ctx context.Context, n *inventory.Node, name string) error {
	if !subName.MatchString(name) {
		return fmt.Errorf("недопустимое имя %q", name)
	}
	_, err := gen(ctx, n, "del", name)
	return err
}

// SubNames — имена клиентов подписки.
func SubNames(ctx context.Context, n *inventory.Node) ([]string, error) {
	out, err := gen(ctx, n, "list")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// TGCard — пользователь MTProxy: ссылки tg:// (открывает Telegram) и https://t.me/proxy (для QR и браузера).
type TGCard struct {
	Name  string `json:"name"`
	TG    string `json:"tg"`
	HTTPS string `json:"https"`
}

// TGClient — add|show|del|list для пользователей MTProxy (реестр на узле, users.json → mtproto).
func TGClient(ctx context.Context, n *inventory.Node, op, name string) (TGCard, []string, error) {
	if op == "list" {
		out, err := gen(ctx, n, "tg-list")
		return TGCard{}, strings.Fields(out), err
	}
	if !subName.MatchString(name) || strings.HasPrefix(name, "_") {
		return TGCard{}, nil, fmt.Errorf("имя: латиница, цифры, . _ -, до 32 знаков, не с «_»")
	}
	out, err := gen(ctx, n, "tg-"+op, name)
	if err != nil || op == "del" {
		return TGCard{}, nil, err
	}
	var c TGCard
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &c); err != nil {
		return c, nil, fmt.Errorf("ответ vpnkit-gen.py не разобрать: %w", err)
	}
	return c, nil, nil
}

// RestoreRegistry кладёт на узел реестр клиентов подписки и Telegram (users.json из резервной
// копии) и пересобирает конфиги: ключи, токены подписок и секреты MTProxy — прежние, клиентам
// ничего перевыдавать не нужно. Текущий реестр перед заменой сохраняется в clients-backup.
func RestoreRegistry(ctx context.Context, n *inventory.Node, data []byte, log func(string)) error {
	var u struct {
		Schema  int            `json:"schema"`
		Clients map[string]any `json:"clients"`
		MTProto map[string]any `json:"mtproto"`
	}
	if err := json.Unmarshal(data, &u); err != nil || u.Schema != 2 {
		return fmt.Errorf("это не реестр krot (schema 2)")
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return err
	}
	defer cl.Close()
	if r, err := cl.RunStdin(ctx, "umask 077; mkdir -p /etc/vpnkit && cat > /etc/vpnkit/.users.restore", string(data)); err != nil || r.Code != 0 {
		return fmt.Errorf("не удалось передать реестр на узел: %v %s", err, r.Stderr)
	}
	r, err := cl.Run(ctx, `set -e
umask 077
f=/etc/vpnkit/.users.restore
python3 -c 'import json,sys;u=json.load(open(sys.argv[1]));assert u.get("schema")==2' "$f" || { rm -f "$f"; echo "на узле файл не прочитался как реестр" >&2; exit 1; }
mkdir -p /var/lib/vpnkit/clients-backup
if [ -f /etc/vpnkit/users.json ]; then cp -a /etc/vpnkit/users.json "/var/lib/vpnkit/clients-backup/users.json.$(date +%Y%m%d-%H%M%S)"; fi
chmod 600 "$f"; mv "$f" /etc/vpnkit/users.json
/usr/local/sbin/vpnkit-gen.py build
# каждая подписка должна отдаваться через 443, как её берут клиенты (поймано 03.10: 403 из-за прав)
if [ -f /etc/vpnkit/sub.env ]; then
  . /etc/vpnkit/sub.env
  for t in $(python3 -c 'import json;[print(c["token"]) for c in json.load(open("/etc/vpnkit/users.json"))["clients"].values()]'); do
    c=$(curl -s -m 10 -o /dev/null -w '%{http_code}' --resolve "$SUB_NAME:443:127.0.0.1" "https://$SUB_NAME/u/$t" || true)
    [ "$c" = 200 ] || { echo "подписка отвечает $c, а не 200" >&2; exit 1; }
  done
fi
`)
	if err != nil || r.Code != 0 {
		return fmt.Errorf("восстановление на узле не удалось: %v %s", err, strings.TrimSpace(r.Stderr))
	}
	tg := 0
	for k := range u.MTProto {
		if !strings.HasPrefix(k, "_") {
			tg++
		}
	}
	log(fmt.Sprintf("реестр возвращён: клиентов подписки %d, Telegram %d", len(u.Clients), tg))
	log("узел: " + strings.TrimSpace(r.Stdout))
	return nil
}
