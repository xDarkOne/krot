// Package alerts — оповещения в Telegram: проверка бота и поиск своего chat id.
// Запросы к api.telegram.org идут С УЗЛА (обычно с выхода): из России без VPN Telegram API
// часто недоступен. Токен передаётся curl через stdin (-K -), а не в аргументах:
// аргументы видны в ps на узле (P-50).
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/remote"
)

// Ключи в хранилище секретов (их же читает модуль monitoring).
const (
	TokenKey = "_global/alert/tg_token"
	ChatsKey = "_global/alert/tg_chats"
)

var (
	tokenRe = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{30,50}$`)
	chatRe  = regexp.MustCompile(`^-?[0-9]{3,20}$`)
)

// ValidToken — похоже ли на токен бота от @BotFather.
func ValidToken(t string) bool { return tokenRe.MatchString(t) }

// ParseChats — список chat id через пробел или запятую.
func ParseChats(s string) ([]string, error) {
	var out []string
	for _, c := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == ';' || r == '\n' }) {
		if !chatRe.MatchString(c) {
			return nil, fmt.Errorf("%q — не chat id (это число, у групп — с минусом)", c)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("нет ни одного получателя")
	}
	return out, nil
}

// call выполняет метод Bot API с узла; тело ответа — JSON Telegram.
func call(ctx context.Context, n *inventory.Node, token, method string, form map[string]string) (map[string]any, error) {
	if !ValidToken(token) {
		return nil, fmt.Errorf("токен не похож на токен бота (вида 123456789:AA…)")
	}
	var cfg strings.Builder
	fmt.Fprintf(&cfg, "url = \"https://api.telegram.org/bot%s/%s\"\n", token, method)
	for k, v := range form {
		fmt.Fprintf(&cfg, "data-urlencode = \"%s=%s\"\n", k, strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`))
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	r, err := cl.RunStdin(ctx, "curl -s -m 20 -K -\n", cfg.String())
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &resp); err != nil {
		if strings.TrimSpace(r.Stdout) == "" {
			return nil, fmt.Errorf("с узла %s Telegram API не ответил (нет связи до api.telegram.org)", n.Name)
		}
		return nil, fmt.Errorf("ответ Telegram не разобрать: %.120s", r.Stdout)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		d, _ := resp["description"].(string)
		switch {
		case strings.Contains(d, "Unauthorized"):
			return resp, fmt.Errorf("Telegram не принял токен: бот удалён или токен перевыпущен")
		case strings.Contains(d, "chat not found"):
			return resp, fmt.Errorf("получатель не найден: напишите боту /start с этого аккаунта")
		case strings.Contains(d, "bot was blocked"):
			return resp, fmt.Errorf("получатель заблокировал бота")
		}
		return resp, fmt.Errorf("Telegram: %s", d)
	}
	return resp, nil
}

// BotName — @имя бота (проверка токена).
func BotName(ctx context.Context, n *inventory.Node, token string) (string, error) {
	r, err := call(ctx, n, token, "getMe", nil)
	if err != nil {
		return "", err
	}
	res, _ := r["result"].(map[string]any)
	u, _ := res["username"].(string)
	return "@" + u, nil
}

// Chat — кто писал боту недавно.
type Chat struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RecentChats — кто писал боту (getUpdates): так человек находит свой chat id —
// пишет боту /start и нажимает «найти».
func RecentChats(ctx context.Context, n *inventory.Node, token string) ([]Chat, error) {
	r, err := call(ctx, n, token, "getUpdates", map[string]string{"limit": "50"})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Chat
	list, _ := r["result"].([]any)
	for _, u := range list {
		um, _ := u.(map[string]any)
		msg, _ := um["message"].(map[string]any)
		chat, _ := msg["chat"].(map[string]any)
		if chat == nil {
			continue
		}
		id := fmt.Sprintf("%.0f", chat["id"])
		if seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(fmt.Sprint(chat["first_name"], " ", chat["last_name"]))
		name = strings.TrimSpace(strings.ReplaceAll(name, "<nil>", ""))
		if t, ok := chat["title"].(string); ok {
			name = t
		}
		if u, ok := chat["username"].(string); ok {
			name += " (@" + u + ")"
		}
		out = append(out, Chat{ID: id, Name: name})
	}
	return out, nil
}

// Send отправляет сообщение каждому получателю; ошибка — по первому, кому не дошло.
func Send(ctx context.Context, n *inventory.Node, token string, chats []string, text string) error {
	for _, c := range chats {
		if _, err := call(ctx, n, token, "sendMessage", map[string]string{"chat_id": c, "text": text}); err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
	}
	return nil
}
