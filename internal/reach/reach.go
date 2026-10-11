// Package reach — проверки «снаружи» с компьютера пользователя (он в России):
// доступен ли сервер без VPN (P-57) и куда смотрят имена в DNS.
package reach

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Verdict — итог проверки доступности.
type Verdict struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	OK       bool   `json:"ok"`
	Banner   string `json:"banner"`   // что ответил сервер (SSH-2.0-…)
	Problem  string `json:"problem"`  // по-человечески, если не OK
	Internet bool   `json:"internet"` // контроль: интернет у пользователя есть
	MyIP     string `json:"myIp"`     // внешний адрес этого компьютера
	ViaOwn   string `json:"viaOwn"`   // этот компьютер выходит через один из своих узлов — проба врёт
	Millis   int64  `json:"ms"`
}

// SSH проверяет, что сервер отвечает настоящим SSH: TCP-соединение и баннер «SSH-2.0».
// Одного SYN-ACK мало — некоторые провайдеры и блокировки отвечают на любой порт.
func SSH(ctx context.Context, host string, port int, ownHosts []string) Verdict {
	v := Verdict{Host: host, Port: port}
	v.Internet = control(ctx)
	v.MyIP = myIP(ctx)
	for _, h := range ownHosts {
		if v.MyIP != "" && h == v.MyIP {
			v.ViaOwn = h
		}
	}
	t0 := time.Now()
	d := net.Dialer{Timeout: 8 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		v.Problem = explain(err, v.Internet)
		return v
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
	line, err := bufio.NewReader(io.LimitReader(c, 256)).ReadString('\n')
	v.Millis = time.Since(t0).Milliseconds()
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "SSH-") {
		if err != nil {
			v.Problem = "соединение открылось, но SSH не ответил (так выглядит блокировка или чужой ответчик на любом порту)"
		} else {
			v.Problem = "на порту отвечает не SSH: " + clip(line)
		}
		return v
	}
	v.Banner, v.OK = clip(line), true
	return v
}

func explain(err error, internet bool) string {
	if !internet {
		return "нет интернета на этом компьютере — проверить сервер нельзя"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "refused"):
		return "сервер отверг соединение: SSH на этом порту не запущен"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		return "сервер не отвечает: из России он недоступен (блокировка по IP) или выключен"
	default:
		return "не удалось соединиться: " + clip(s)
	}
}

// control — есть ли у пользователя интернет вообще: без этого «недоступен» ничего не значит.
func control(ctx context.Context) bool {
	for _, hp := range []string{"77.88.8.8:53", "1.1.1.1:443", "8.8.8.8:443"} {
		d := net.Dialer{Timeout: 4 * time.Second}
		if c, err := d.DialContext(ctx, "tcp", hp); err == nil {
			c.Close()
			return true
		}
	}
	return false
}

// myIP — внешний адрес компьютера по мнению российского сервиса (Яндекс).
func myIP(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://ipv4-internet.yandex.net/api/v0/ip", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64))
	ip := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// DNSAnswer — что один публичный резолвер отвечает на имя.
type DNSAnswer struct {
	Resolver string   `json:"resolver"`
	Addrs    []string `json:"addrs"`
	Error    string   `json:"error,omitempty"`
}

// DNSResult — итог проверки имени.
type DNSResult struct {
	Name    string      `json:"name"`
	Want    string      `json:"want"`
	OK      bool        `json:"ok"` // все резолверы отвечают ровно нужным адресом
	Answers []DNSAnswer `json:"answers"`
	Problem string      `json:"problem"`
}

// Resolvers — публичные резолверы: зарубежные и российский (у клиентов в РФ может быть любой).
var Resolvers = []string{"1.1.1.1", "8.8.8.8", "77.88.8.8"}

// DNS проверяет A-записи имени у нескольких резолверов: все должны отвечать адресом want.
// Лишний адрес (например, прокси Cloudflare) — ошибка: сертификат и туннели идут на сервер напрямую.
func DNS(ctx context.Context, name, want string) DNSResult {
	res := DNSResult{Name: name, Want: want, OK: true}
	for _, srv := range Resolvers {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, "udp", net.JoinHostPort(srv, "53"))
		}}
		cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		ips, err := r.LookupIP(cctx, "ip4", name)
		cancel()
		a := DNSAnswer{Resolver: srv}
		if err != nil {
			a.Error = "нет записи"
			if !strings.Contains(err.Error(), "no such host") {
				a.Error = clip(err.Error())
			}
			res.OK = false
		}
		for _, ip := range ips {
			a.Addrs = append(a.Addrs, ip.String())
		}
		if err == nil && !(len(a.Addrs) == 1 && a.Addrs[0] == want) {
			res.OK = false
		}
		res.Answers = append(res.Answers, a)
	}
	if !res.OK {
		res.Problem = dnsProblem(res)
	}
	return res
}

func dnsProblem(r DNSResult) string {
	none, other := 0, map[string]bool{}
	for _, a := range r.Answers {
		if a.Error != "" {
			none++
		}
		for _, ip := range a.Addrs {
			if ip != r.Want {
				other[ip] = true
			}
		}
	}
	switch {
	case none == len(r.Answers):
		return fmt.Sprintf("записи %s ещё нет: создайте A-запись → %s (без прокси Cloudflare)", r.Name, r.Want)
	case len(other) > 0:
		var o []string
		cf := false
		for ip := range other {
			o = append(o, ip)
			cf = cf || IsCloudflare(ip)
		}
		if cf {
			return fmt.Sprintf("%s проксируется Cloudflare (%s) — выключите проксирование, облако должно быть серым: туннелям и сертификату нужен сам сервер %s", r.Name, strings.Join(o, ", "), r.Want)
		}
		return fmt.Sprintf("%s указывает на %s, а нужно %s — исправьте A-запись", r.Name, strings.Join(o, ", "), r.Want)
	default:
		return "запись видна не везде — подождите несколько минут, DNS обновляется"
	}
}

// Диапазоны IPv4 Cloudflare (https://www.cloudflare.com/ips-v4/, на 03.10.2026).
var cloudflareV4 = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22", "141.101.64.0/18",
	"108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20", "197.234.240.0/22", "198.41.128.0/17",
	"162.158.0.0/15", "104.16.0.0/13", "104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

// IsCloudflare — адрес из сетей Cloudflare (имя проксируется, «оранжевое облако»).
func IsCloudflare(ip string) bool {
	a := net.ParseIP(ip)
	for _, c := range cloudflareV4 {
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(a) {
			return true
		}
	}
	return false
}
