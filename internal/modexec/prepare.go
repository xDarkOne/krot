package modexec

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strings"

	"github.com/xDarkOne/krot/agent"
	"github.com/xDarkOne/krot/internal/awgkeys"
	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/secrets"
)

// telegramPrefixes — сети Telegram: entry ходит к ним через каскад (бот, telemt).
var telegramPrefixes = "91.105.192.0/23 91.108.4.0/22 91.108.8.0/22 91.108.12.0/22 91.108.16.0/22 " +
	"91.108.20.0/22 91.108.56.0/22 95.161.64.0/20 149.154.160.0/20 185.76.151.0/24"

// Prepare — параметры модуля для узла: базовые (Params) плюс то, что пульт должен
// согласовать между узлами и хранить в секретах (ключи, порты, обфускация).
// Новые секреты сохраняются в хранилище до установки — повторный запуск даст те же значения.
func Prepare(inv *inventory.Inventory, n *inventory.Node, module string, sec *secrets.Store) (map[string]string, error) {
	p := Params(n, module)
	switch module {
	case "cascade-awg0":
		return prepareCascade(inv, n, p, sec)
	case "client-awg":
		return prepareClientAWG(n, p, sec)
	case "amneziawg":
		return prepareAmneziaWG(p)
	case "nginx-tls":
		return prepareNginxTLS(n, p, sec)
	case "xray":
		return prepareXray(inv, n, p, sec)
	case "hysteria2":
		return prepareHysteria2(n, p)
	case "subscriptions":
		return prepareSubscriptions(n, p)
	case "mtproxy":
		return prepareMTProxy(n, p, sec)
	case "backup":
		return prepareBackup(n, p)
	case "monitoring":
		return prepareMonitoring(inv, n, p, sec)
	}
	return p, nil
}

// pair находит пару entry↔exit для каскада (ровно по одному узлу каждой роли).
func pair(inv *inventory.Inventory) (entry, exit *inventory.Node, err error) {
	for _, x := range inv.Nodes {
		switch x.Role {
		case "entry":
			if entry != nil {
				return nil, nil, fmt.Errorf("в inventory больше одного entry")
			}
			entry = x
		case "exit":
			if exit != nil {
				return nil, nil, fmt.Errorf("в inventory больше одного exit")
			}
			exit = x
		}
	}
	if entry == nil || exit == nil {
		return nil, nil, fmt.Errorf("для каскада нужны узлы entry и exit")
	}
	return entry, exit, nil
}

// ensure возвращает секрет из хранилища или создаёт его функцией gen.
func ensure(sec *secrets.Store, key string, gen func() (string, error), changed *bool) (string, error) {
	if v, ok := sec.Get(key); ok {
		return v, nil
	}
	v, err := gen()
	if err != nil {
		return "", err
	}
	if err := sec.Set(key, v); err != nil {
		return "", err
	}
	*changed = true
	return v, nil
}

func prepareCascade(inv *inventory.Inventory, n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	entry, exit, err := pair(inv)
	if err != nil {
		return nil, err
	}
	if n != entry && n != exit {
		return nil, fmt.Errorf("cascade-awg0 ставится только на entry и exit")
	}
	cfg := exit.Modules["cascade-awg0"]
	tunNet := "10.100.0.0/30"
	if v, ok := cfg["tunnel_net"]; ok {
		tunNet = fmt.Sprint(v)
	}
	ip, ipnet, err := net.ParseCIDR(tunNet)
	if err != nil || ip.To4() == nil {
		return nil, fmt.Errorf("tunnel_net %q: нужна IPv4-сеть", tunNet)
	}
	base := ipnet.IP.To4()
	exitIP := net.IPv4(base[0], base[1], base[2], base[3]+1).String()
	entryIP := net.IPv4(base[0], base[1], base[2], base[3]+2).String()
	ones, _ := ipnet.Mask.Size()

	ns := exit.Name + "/cascade-awg0/"
	changed := false
	exitKey, err := ensure(sec, ns+"exit_private", func() (string, error) { k, e := awgkeys.NewKeypair(); return k.Private, e }, &changed)
	if err != nil {
		return nil, err
	}
	entryKey, err := ensure(sec, ns+"entry_private", func() (string, error) { k, e := awgkeys.NewKeypair(); return k.Private, e }, &changed)
	if err != nil {
		return nil, err
	}
	psk, err := ensure(sec, ns+"psk", awgkeys.NewSymmetric, &changed)
	if err != nil {
		return nil, err
	}
	hpk, err := ensure(sec, ns+"hpk", awgkeys.NewSymmetric, &changed)
	if err != nil {
		return nil, err
	}
	port, err := ensure(sec, ns+"port", func() (string, error) {
		if v, ok := cfg["port"]; ok {
			return fmt.Sprint(v), nil
		}
		return fmt.Sprint(20000 + int(randInt(40000))), nil
	}, &changed)
	if err != nil {
		return nil, err
	}
	obfsJSON, err := ensure(sec, ns+"obfs", func() (string, error) {
		b, e := json.Marshal(awgkeys.NewObfs())
		return string(b), e
	}, &changed)
	if err != nil {
		return nil, err
	}
	var obfs awgkeys.Obfs
	if err := json.Unmarshal([]byte(obfsJSON), &obfs); err != nil {
		return nil, fmt.Errorf("секрет %sobfs повреждён: %w", ns, err)
	}
	if err := obfs.Valid(); err != nil {
		return nil, fmt.Errorf("обфускация каскада недопустима: %w", err)
	}
	if changed {
		if err := sec.Save(); err != nil {
			return nil, fmt.Errorf("сохранить секреты каскада: %w", err)
		}
	}
	exitPub, err := awgkeys.PublicOf(exitKey)
	if err != nil {
		return nil, err
	}
	entryPub, err := awgkeys.PublicOf(entryKey)
	if err != nil {
		return nil, err
	}

	p["TUN_NET"] = tunNet
	p["MTU"] = "1390"
	p["PORT"] = port
	p["PSK"] = psk
	p["HPK"] = hpk
	p["OBFS"] = strings.TrimRight(obfs.Lines(), "\n")
	p["EXIT_IP"] = exit.Host
	if n == exit {
		p["SIDE"] = "server"
		p["ADDR"] = fmt.Sprintf("%s/%d", exitIP, ones)
		p["PEER_TUN"] = entryIP
		p["PRIVATE"] = exitKey
		p["PEER_PUBLIC"] = entryPub
	} else {
		p["SIDE"] = "client"
		p["ADDR"] = fmt.Sprintf("%s/%d", entryIP, ones)
		p["PEER_TUN"] = exitIP
		p["PRIVATE"] = entryKey
		p["PEER_PUBLIC"] = exitPub
		p["ENDPOINT"] = net.JoinHostPort(exit.Host, port)
		p["TG_PREFIXES"] = telegramPrefixes
	}
	return p, nil
}

// SecretParams — какие параметры не печатать в плане и журналах.
var SecretParams = map[string]bool{"PRIVATE": true, "PRIV": true, "PSK": true, "HPK": true, "TG_TOKEN": true}

// randInt — криптостойкое случайное число в [0, n).
func randInt(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic(err)
	}
	return v.Int64()
}

// prepareClientAWG — клиентский вход AmneziaWG на entry: ключ сервера, HPK, порт, обфускация
// (своя, отличная от каскада). Секреты: <узел>/client-awg/<iface>/*.
func prepareClientAWG(n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	cfg := n.Modules["client-awg"]
	get := func(k, def string) string {
		if v, ok := cfg[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	iface := get("iface", "awg2")
	netw := get("net", "10.9.2.0/24")
	ip, ipnet, err := net.ParseCIDR(netw)
	if err != nil || ip.To4() == nil {
		return nil, fmt.Errorf("client-awg net %q: нужна IPv4-сеть", netw)
	}
	b := ipnet.IP.To4()
	gw := net.IPv4(b[0], b[1], b[2], b[3]+1).String()

	ns := n.Name + "/client-awg/" + iface + "/"
	changed := false
	priv, err := ensure(sec, ns+"private", func() (string, error) { k, e := awgkeys.NewKeypair(); return k.Private, e }, &changed)
	if err != nil {
		return nil, err
	}
	hpk, err := ensure(sec, ns+"hpk", awgkeys.NewSymmetric, &changed)
	if err != nil {
		return nil, err
	}
	port, err := ensure(sec, ns+"port", func() (string, error) {
		if v, ok := cfg["port"]; ok {
			return fmt.Sprint(v), nil
		}
		return fmt.Sprint(20000 + int(randInt(40000))), nil
	}, &changed)
	if err != nil {
		return nil, err
	}
	obfsJSON, err := ensure(sec, ns+"obfs", func() (string, error) {
		b, e := json.Marshal(awgkeys.NewObfsWith(awgkeys.ClientI))
		return string(b), e
	}, &changed)
	if err != nil {
		return nil, err
	}
	var obfs awgkeys.Obfs
	if err := json.Unmarshal([]byte(obfsJSON), &obfs); err != nil {
		return nil, fmt.Errorf("секрет %sobfs повреждён: %w", ns, err)
	}
	if err := obfs.Valid(); err != nil {
		return nil, fmt.Errorf("обфускация входа недопустима: %w", err)
	}
	if changed {
		if err := sec.Save(); err != nil {
			return nil, err
		}
	}
	p["IFACE"] = iface
	p["NET"] = netw
	p["GW"] = gw
	p["PORT"] = port
	p["MTU"] = get("mtu", "1260")
	p["MSS"] = get("mss", "1220")
	p["PRIVATE"] = priv
	p["HPK"] = hpk
	p["OBFS"] = strings.TrimRight(obfs.Lines(), "\n")
	p["RULE_MAIN"] = get("rule_main", "208")
	p["RULE_CASCADE"] = get("rule_cascade", "212")
	// вход каскада — российское напрямую, остальное в awg0; один сервер — всё напрямую
	p["CASCADE"] = map[bool]string{true: "1", false: "0"}[n.Role == "entry"]
	return p, nil
}

// prepareBackup — копии шифруются на открытый ключ пульта: расшифровать их может только пульт.
func prepareBackup(n *inventory.Node, p map[string]string) (map[string]string, error) {
	id, err := secrets.LoadKey()
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, fmt.Errorf("нет ключа пульта %s — создайте его: krot secrets init --key", secrets.KeyPath())
	}
	p["NODE"] = n.Name
	p["RECIPIENT"] = id.Recipient().String()
	if _, ok := p["TIME"]; !ok {
		p["TIME"] = map[string]string{"exit": "04:00"}[n.Role]
		if p["TIME"] == "" {
			p["TIME"] = "04:30"
		}
	}
	return p, nil
}

// Оповещения в Telegram — общие для всех узлов секреты; без них проверки только пишутся в журнал.
const (
	AlertTokenKey = "_global/alert/tg_token"
	AlertChatsKey = "_global/alert/tg_chats"
)

// prepareMonitoring — сосед по каскаду (адрес в туннеле, публичный IP выхода) и токен оповещений.
func prepareMonitoring(inv *inventory.Inventory, n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	p["NODE"] = n.Name
	if entry, exit, err := pair(inv); err == nil && (n == entry || n == exit) {
		tunNet := "10.100.0.0/30"
		if v, ok := exit.Modules["cascade-awg0"]["tunnel_net"]; ok {
			tunNet = fmt.Sprint(v)
		}
		_, ipnet, err := net.ParseCIDR(tunNet)
		if err != nil || ipnet.IP.To4() == nil {
			return nil, fmt.Errorf("tunnel_net %q: нужна IPv4-сеть", tunNet)
		}
		b := ipnet.IP.To4()
		if n == entry {
			p["PEER_NAME"], p["PEER_TUN"] = exit.Name, net.IPv4(b[0], b[1], b[2], b[3]+1).String()
			p["EXIT_IP"] = exit.Host
		} else {
			p["PEER_NAME"], p["PEER_TUN"] = entry.Name, net.IPv4(b[0], b[1], b[2], b[3]+2).String()
			p["PEER_PUBLIC"] = entry.Host
		}
	}
	tok, ok1 := sec.Get(AlertTokenKey)
	chats, ok2 := sec.Get(AlertChatsKey)
	if ok1 && ok2 {
		p["TG_TOKEN"], p["TG_CHATS"] = tok, chats
	}
	return p, nil
}

// siteVariants — тексты витрин. Узлу достаётся один случайный вариант, номер хранится в
// секретах, чтобы витрина не менялась от установки к установке.
var siteVariants = []map[string]string{
	{"LANG": "en", "TITLE": "Northlight Studio", "TAGLINE": "Architectural photography",
		"P1": "We photograph buildings, interiors and the people who make them, for architects and publishers across Europe.",
		"P2": "Bookings for the next season are open. Portfolio available on request."},
	{"LANG": "es", "TITLE": "Taller Almendro", "TAGLINE": "Cerámica hecha a mano",
		"P1": "Piezas de gres y porcelana torneadas a mano en pequeñas series.",
		"P2": "Encargos y talleres de fin de semana: escríbenos para conocer las fechas."},
	{"LANG": "de", "TITLE": "Kanzlei Weber & Roth", "TAGLINE": "Steuerberatung",
		"P1": "Wir begleiten Selbstständige und kleine Unternehmen bei Buchhaltung, Jahresabschluss und Steuererklärung.",
		"P2": "Termine nach Vereinbarung."},
	{"LANG": "en", "TITLE": "Harbour & Pine", "TAGLINE": "Guest house by the sea",
		"P1": "Six quiet rooms, breakfast from the local bakery and a garden that opens onto the dunes.",
		"P2": "Open from April to October."},
}

// names — значение параметра-списка (в inventory это YAML-список или строка через пробел).
func names(v any) []string {
	switch x := v.(type) {
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case nil:
		return nil
	default:
		return strings.Fields(fmt.Sprint(x))
	}
}

// prepareNginxTLS — имена, адрес узла (для проверки DNS) и витрина.
func prepareNginxTLS(n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	ns := names(n.Modules["nginx-tls"]["names"])
	if len(ns) == 0 {
		return nil, fmt.Errorf("nginx-tls: в inventory не заданы имена (modules.nginx-tls.names) — DNS A каждого имени должен указывать на %s", n.Host)
	}
	for _, x := range ns {
		if !validName(x) {
			return nil, fmt.Errorf("nginx-tls: %q — не доменное имя", x)
		}
	}
	p["NAMES"] = strings.Join(ns, " ")
	p["HOST"] = n.Host
	if t, ok := p["TUNNEL_NAME"]; ok && !contains(ns, t) {
		return nil, fmt.Errorf("nginx-tls: tunnel_name %q нет среди names", t)
	}
	changed := false
	idx, err := ensure(sec, n.Name+"/nginx-tls/site", func() (string, error) { return fmt.Sprint(randInt(int64(len(siteVariants)))), nil }, &changed)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := sec.Save(); err != nil {
			return nil, err
		}
	}
	var i int
	fmt.Sscan(idx, &i)
	for k, v := range siteVariants[i%len(siteVariants)] {
		p["SITE_"+k] = v
	}
	return p, nil
}

func validName(s string) bool {
	if len(s) < 4 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, r := range s {
		if !(r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

// Пины бинарников: версия и sha256 архива релиза (сверено с .dgst XTLS).
const (
	XrayVersion      = "26.3.27"
	xraySHA256AMD64  = "23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae"
	xraySHA256ARM64  = "4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c"
	SingBoxVersion   = "1.13.18"
	singboxSHA256AMD = "c011a13c48e1fe66e39973b1dd7ec01d61ac7a0ac9e541024be9d452121c5761"
	singboxSHA256ARM = "49755895316da3330cb962f4299354a356c7ddc971e263cba38c0a369bb36a1f"
)

// tunnelName — имя, на котором живут туннели: nginx-tls.tunnel_name или первое из names.
func tunnelName(n *inventory.Node) (string, error) {
	cfg := n.Modules["nginx-tls"]
	if v, ok := cfg["tunnel_name"]; ok {
		return fmt.Sprint(v), nil
	}
	if ns := names(cfg["names"]); len(ns) > 0 {
		return ns[0], nil
	}
	return "", fmt.Errorf("у узла %s нет имён nginx-tls — сначала nginx-tls", n.Name)
}

func xhttpPort(n *inventory.Node) string {
	if v, ok := n.Modules["nginx-tls"]["xhttp_port"]; ok {
		return fmt.Sprint(v)
	}
	return "4443"
}

// prepareXray — пин, имя туннеля, случайные порты и пути (хранятся в секретах — не меняются
// между установками, иначе у клиентов разъедутся ссылки), каскад и адрес выхода для проверки.
func prepareXray(inv *inventory.Inventory, n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	tn, err := tunnelName(n)
	if err != nil {
		return nil, err
	}
	p["TUNNEL_NAME"], p["XHTTP_PORT"] = tn, xhttpPort(n)
	p["XRAY_VERSION"], p["XRAY_SHA256_AMD64"], p["XRAY_SHA256_ARM64"] = XrayVersion, xraySHA256AMD64, xraySHA256ARM64
	ns := n.Name + "/xray/"
	changed := false
	gen := map[string]func() (string, error){
		"xh_port":  func() (string, error) { return fmt.Sprint(11000 + randInt(9000)), nil },
		"xh_path":  func() (string, error) { return "/" + pick(staticDirs) + "/" + randHex(3), nil },
		"enc_port": func() (string, error) { return fmt.Sprint(30000 + randInt(30000)), nil },
		"enc_path": func() (string, error) { return "/" + randHex(2), nil },
	}
	for _, k := range []string{"xh_port", "xh_path", "enc_port", "enc_path"} {
		v, err := ensure(sec, ns+k, gen[k], &changed)
		if err != nil {
			return nil, err
		}
		p[strings.ToUpper(k)] = v
	}
	if changed {
		if err := sec.Save(); err != nil {
			return nil, err
		}
	}
	p["SOCKS_PORT"] = "10801"
	if _, ok := p["LABEL"]; !ok {
		p["LABEL"] = n.Name
	}
	p["CASCADE"] = "0"
	if n.Role == "entry" {
		_, exit, err := pair(inv)
		if err != nil {
			return nil, err
		}
		p["CASCADE"], p["EXIT_IP"] = "1", exit.Host
	}
	return p, nil
}

var staticDirs = []string{"static", "assets", "media", "files", "cdn", "res"}

func pick(a []string) string { return a[randInt(int64(len(a)))] }

func randHex(nbytes int) string {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", b)
}

// prepareHysteria2 — пин sing-box и имя туннеля (сертификат берётся у nginx-tls).
func prepareHysteria2(n *inventory.Node, p map[string]string) (map[string]string, error) {
	tn, err := tunnelName(n)
	if err != nil {
		return nil, err
	}
	p["TUNNEL_NAME"] = tn
	if _, ok := p["HY2_PORT"]; !ok {
		p["HY2_PORT"] = "443"
	}
	p["SINGBOX_VERSION"], p["SINGBOX_SHA256_AMD64"], p["SINGBOX_SHA256_ARM64"] = SingBoxVersion, singboxSHA256AMD, singboxSHA256ARM
	return p, nil
}

// prepareSubscriptions — имя для раздачи: subscriptions.sub_name или имя туннеля; оно должно
// быть среди имён nginx-tls (там его витрина и сертификат).
func prepareSubscriptions(n *inventory.Node, p map[string]string) (map[string]string, error) {
	tn, err := tunnelName(n)
	if err != nil {
		return nil, err
	}
	if _, ok := p["SUB_NAME"]; !ok {
		p["SUB_NAME"] = tn
	}
	if !contains(names(n.Modules["nginx-tls"]["names"]), p["SUB_NAME"]) {
		return nil, fmt.Errorf("subscriptions: sub_name %q нет среди имён nginx-tls", p["SUB_NAME"])
	}
	// routing: true/false в inventory → 1/0 для скрипта; по умолчанию включено
	switch strings.ToLower(p["ROUTING"]) {
	case "false", "0", "no", "off":
		p["ROUTING"] = "0"
	default:
		p["ROUTING"] = "1"
	}
	return p, nil
}

// telemt — официальный релиз (sha256 архивов = их .sha256 в релизе). Код тот же, что в MTProxyL:
// там 3.5.7 без патчей, собранный MTProxyL в своём образе (сверено 02.10.2026 по строкам бинарника).
const (
	TelemtVersion   = "3.5.7"
	telemtSHA256AMD = "c88656514164dbae64aac68341548b95586a3898df851dad837973752e405864"
	telemtSHA256ARM = "2cd4d501d0524de7deec0fa5de7665d784c653c50d03cdb90ce7b6b88bc270c7"
)

// MTProxyL (github.com/Liafanx/MTProxyL) — вид MTProxy по умолчанию:
// бот, панель, WEB Proxy, перезапуск при падении DC, проверка доступности. Ставится по тегу,
// движок — бинарник telemt под systemd (без Docker), версия закреплена.
const (
	MTProxyLTag    = "v1.6.37"
	MTProxyLEngine = "3.5.13"
)

// MTProxyBotTokenKey — токен бота MTProxyL в хранилище (свой бот: оповещения krot при
// настройке читают getUpdates и отобрали бы сообщения у общего).
func MTProxyBotTokenKey(node string) string { return node + "/mtproxy/tgbot_token" }

// webName — имя для WEB Proxy: mtproxy.web_name или имя nginx-tls, чья первая метка web / *-web.
func webName(ns []string, given string) string {
	if given != "" {
		return given
	}
	for _, x := range ns {
		l := strings.SplitN(x, ".", 2)[0]
		if l == "web" || strings.HasSuffix(l, "-web") {
			return x
		}
	}
	return ""
}

// prepareMTProxy — имя маски FakeTLS (mtproxy.mask_name или первое имя nginx-tls: у него есть
// витрина и сертификат), вид (backend: mtproxyl | telemt), пины, каскад для STUN.
func prepareMTProxy(n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	ns := names(n.Modules["nginx-tls"]["names"])
	if len(ns) == 0 {
		return nil, fmt.Errorf("у узла %s нет имён nginx-tls — сначала nginx-tls", n.Name)
	}
	if _, ok := p["MASK_NAME"]; !ok {
		p["MASK_NAME"] = ns[0]
	}
	if !contains(ns, p["MASK_NAME"]) {
		return nil, fmt.Errorf("mtproxy: mask_name %q нет среди имён nginx-tls", p["MASK_NAME"])
	}
	p["TLS_DOMAIN"], p["PUBLIC_HOST"] = p["MASK_NAME"], p["MASK_NAME"]
	p["TELEMT_VERSION"], p["TELEMT_SHA256_AMD64"], p["TELEMT_SHA256_ARM64"] = TelemtVersion, telemtSHA256AMD, telemtSHA256ARM
	p["CASCADE"] = map[bool]string{true: "1", false: "0"}[n.Role == "entry"]
	if p["BACKEND"] == "" {
		p["BACKEND"] = "mtproxyl"
	}
	switch p["BACKEND"] {
	case "telemt":
		return p, nil
	case "mtproxyl":
	default:
		return nil, fmt.Errorf("mtproxy: backend %q — бывает mtproxyl или telemt", p["BACKEND"])
	}
	// MTProxyL за нашим nginx на 443 работает в раскладке WEB Proxy (telemt на 127.0.0.1:15443 с
	// PROXY protocol, WEB — 15080 за TLS-сервером nginx): без WEB он сел бы на 443 сам.
	web := webName(ns, p["WEB_NAME"])
	if web == "" {
		return nil, fmt.Errorf("mtproxy: MTProxyL нужно имя для WEB Proxy — добавьте web.<домен> в nginx-tls.names (DNS A → %s) или задайте mtproxy.web_name", n.Host)
	}
	if !contains(ns, web) {
		return nil, fmt.Errorf("mtproxy: web_name %q нет среди имён nginx-tls (сертификат выпускает он)", web)
	}
	if web == p["MASK_NAME"] {
		return nil, fmt.Errorf("mtproxy: имя WEB Proxy %q совпадает с маской FakeTLS — нужно отдельное", web)
	}
	p["WEB_DOMAIN"], p["MTPROXYL_TAG"], p["TELEMT_VERSION"] = web, MTProxyLTag, MTProxyLEngine
	if tok, ok := sec.Get(MTProxyBotTokenKey(n.Name)); ok && tok != "" {
		if p["TGBOT_ADMIN"] == "" {
			return nil, fmt.Errorf("mtproxy: токен бота MTProxyL есть, а mtproxy.tgbot_admin (числовой id) не задан")
		}
		p["TGBOT_TOKEN"] = tok
	}
	return p, nil
}

// pins — закреплённые версии, от которых зависит модуль: смена пина — тоже новая версия модуля.
func pins(module string) string {
	switch module {
	case "xray":
		return XrayVersion + xraySHA256AMD64 + xraySHA256ARM64
	case "hysteria2":
		return SingBoxVersion + singboxSHA256AMD + singboxSHA256ARM
	case "mtproxy":
		return TelemtVersion + telemtSHA256AMD + telemtSHA256ARM + MTProxyLTag + MTProxyLEngine
	case "amneziawg":
		return AWGTag
	}
	return ""
}

// CodeHash — версия модуля в этом пульте (скрипты + общие файлы + пины). Её пишет метка на узле.
func CodeHash(module string) string { return agent.Hash(module, pins(module)) }

// AWGTag — тег модуля ядра AmneziaWG (git, DKMS), AWGToolsTag — утилит; закреплены как остальные пины.
// Модуль 3.1.20260906 (проверен 10.2026); утилит новее 3.1.20260812 нет.
const (
	AWGTag      = "v3.1.20260906"
	AWGToolsTag = "v3.1.20260812"
)

// prepareAmneziaWG — пин тега; module_tag/tools_tag в inventory переопределяют (для проверки новой версии).
func prepareAmneziaWG(p map[string]string) (map[string]string, error) {
	mt, tt := p["MODULE_TAG"], p["TOOLS_TAG"] // из inventory: module_tag, tools_tag
	p["TAG"], p["TOOLS_TAG"] = AWGTag, AWGToolsTag
	if mt != "" {
		p["TAG"] = mt
	}
	if tt != "" {
		p["TOOLS_TAG"] = tt
	}
	return p, nil
}
