package clients

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/skip2/go-qrcode"

	"github.com/xDarkOne/vpnkit/internal/remote"
)

// Card — то, что получает человек: текст файла, содержимое QR и подсказка, чем открыть.
// Одна карточка и для пульта (файлы в ~/.vpnkit/clients/<узел>/), и для приложения (экран).
type Card struct {
	Kind  string `json:"kind"` // awg | sub | tg
	Node  string `json:"node"`
	Name  string `json:"name"`
	Title string `json:"title"` // что это, по-человечески
	QR    string `json:"qr"`    // что зашито в QR
	Body  string `json:"body"`  // текст файла: ссылки с пояснениями
	Hint  string `json:"hint"`  // чем открыть
	Extra string `json:"extra"` // доп. файл (профиль .conf для AWG), может быть пустым
	Base  string `json:"base"`  // путь к файлам без расширения (после Save)
}

// Kinds — расширения файлов карточки по виду клиента.
var kindExt = map[string][]string{
	"awg": {".conf", ".vpn.txt", ".qr.png"},
	"sub": {".sub.txt", ".sub.qr.png"},
	"tg":  {".tg.txt", ".tg.qr.png"},
}

// Dir — каталог файлов клиентов узла на пульте.
func Dir(node string) string { return filepath.Join(remote.StateDir(), "clients", node) }

// AWGCard — карточка клиента AmneziaWG.
func AWGCard(node string, p Profile) Card {
	return Card{Kind: "awg", Node: node, Name: p.Name, Title: "AmneziaWG · " + p.IP,
		QR: p.Link, Body: p.Link + "\n", Extra: p.Conf,
		Hint: "AmneziaVPN: отсканировать QR или вставить ссылку vpn://; AmneziaWG: импорт файла .conf"}
}

// SubCardOf — карточка клиента подписки.
func SubCardOf(node string, c SubCard) Card {
	body := "# подписка (Happ, v2rayNG, Hiddify, Streisand, sing-box: «добавить по ссылке»)\n" + c.SubURL +
		"\n\n# отдельные входы — если приложение не берёт подписку\n" + strings.Join(c.Links, "\n") + "\n"
	if c.Routing["happ"] != "" {
		body += "\n# правила «российское — напрямую» (открыть на телефоне, приложение добавит их само)\n" +
			"# Happ: " + c.Routing["happ"] + "\n# INCY: " + c.Routing["incy"] + "\n"
	}
	qr := c.SubURL
	if qr == "" && len(c.Links) > 0 { // подписка не включена — в QR первая ссылка
		qr = c.Links[0]
	}
	return Card{Kind: "sub", Node: node, Name: c.Name, Title: "Подписка · входов " + strconv.Itoa(len(c.Links)),
		QR: qr, Body: body, Hint: "Happ, v2rayNG, Hiddify, Streisand: «добавить подписку» — QR или ссылка"}
}

// TGCardOf — карточка пользователя Telegram-прокси.
func TGCardOf(node string, c TGCard) Card {
	body := "# Telegram-прокси: открыть ссылку на телефоне или отсканировать QR камерой\n" + c.HTTPS +
		"\n\n# то же для приложения Telegram на компьютере\n" + c.TG + "\n"
	return Card{Kind: "tg", Node: node, Name: c.Name, Title: "Telegram-прокси",
		QR: c.HTTPS, Body: body, Hint: "Камера телефона → откроется Telegram → «Подключить прокси»"}
}

// QRPNG — QR карточки в PNG.
func (c Card) QRPNG(size int) ([]byte, error) {
	lvl := qrcode.Medium
	if len(c.QR) > 600 { // профиль vpn:// длинный — меньше избыточности, иначе QR не читается
		lvl = qrcode.Low
	}
	return qrcode.Encode(c.QR, lvl, size)
}

// Save кладёт файлы карточки в ~/.vpnkit/clients/<узел>/ (права 600: там ключи клиента).
func (c *Card) Save() error {
	dir := Dir(c.Node)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	c.Base = filepath.Join(dir, c.Name)
	ext := kindExt[c.Kind]
	png, err := c.QRPNG(768)
	if err != nil {
		return err
	}
	if c.Kind == "awg" {
		if err := os.WriteFile(c.Base+".conf", []byte(c.Extra), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(c.Base+".vpn.txt", []byte(c.Body), 0o600); err != nil {
			return err
		}
	} else if err := os.WriteFile(c.Base+ext[0], []byte(c.Body), 0o600); err != nil {
		return err
	}
	return os.WriteFile(c.Base+ext[len(ext)-1], png, 0o600)
}

// RemoveFiles удаляет файлы карточки клиента на пульте.
func RemoveFiles(node, kind, name string) {
	for _, ext := range kindExt[kind] {
		os.Remove(filepath.Join(Dir(node), name+ext))
	}
}
