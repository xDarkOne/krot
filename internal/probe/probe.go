// Package probe собирает факты об узле (только чтение) и выносит вердикт,
// подходит ли узел под роль. Пороги — из docs/40-requirements.md.
package probe

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/xDarkOne/vpnkit/internal/remote"
)

// Facts — пары ключ=значение, которые печатает скрипт сбора.
type Facts map[string]string

// Int возвращает числовой факт (0, если нет).
func (f Facts) Int(k string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(f[k]))
	return n
}

// Скрипт для серверов. Только чтение. Каждая строка — key=value.
const serverScript = `
. /etc/os-release 2>/dev/null
echo "os=$ID"
echo "os_version=$VERSION_ID"
echo "kernel=$(uname -r)"
echo "arch=$(uname -m)"
echo "virt=$(systemd-detect-virt 2>/dev/null || echo unknown)"
echo "cpus=$(nproc)"
echo "mem_mb=$(awk '/MemTotal/{print int($2/1024)}' /proc/meminfo)"
echo "swap_mb=$(awk '/SwapTotal/{print int($2/1024)}' /proc/meminfo)"
echo "disk_free_mb=$(df -Pm / | awk 'NR==2{print $4}')"
echo "headers=$(apt-cache policy linux-headers-$(uname -r) 2>/dev/null | awk '/Candidate/{print $2}')"
echo "secureboot=$(mokutil --sb-state 2>/dev/null | head -1)"
echo "tcp_listen=$(ss -Hltn 2>/dev/null | awk '{print $4}' | sed 's/.*://' | sort -un | tr '\n' ' ')"
echo "udp_listen=$(ss -Hlun 2>/dev/null | awk '{print $4}' | sed 's/.*://' | sort -un | tr '\n' ' ')"
echo "docker=$(command -v docker >/dev/null 2>&1 && echo yes || echo no)"
echo "containers=$(docker ps --format '{{.Names}}' 2>/dev/null | tr '\n' ' ')"
echo "if_addrs=$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | tr '\n' ' ')"
echo "public_ip=$(curl -s -m6 https://api.ipify.org 2>/dev/null)"
echo "reboot_required=$([ -f /var/run/reboot-required ] && echo yes || echo no)"
echo "awg_module=$(modinfo -F version amneziawg 2>/dev/null)"
echo "xray=$(xray version 2>/dev/null | head -1 | cut -d' ' -f2)"
echo "p443=$(ss -Hltnp 'sport = :443' 2>/dev/null | grep -o 'users:(("[^"]*' | head -1 | cut -d'"' -f2)"
echo "flush_ruleset=$(grep -c '^flush ruleset' /etc/nftables.conf 2>/dev/null)"
`

// Collect запускает сбор фактов на узле.
func Collect(ctx context.Context, cl *remote.Client) (Facts, error) {
	r, err := cl.Run(ctx, serverScript)
	if err != nil {
		return nil, err
	}
	f := Facts{}
	sc := bufio.NewScanner(strings.NewReader(r.Stdout))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '='); i > 0 {
			f[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return f, nil
}

// Level — важность находки.
type Level int

const (
	OK Level = iota
	Warn
	Fail
)

func (l Level) String() string {
	return [...]string{"ок", "внимание", "НЕ ПОДХОДИТ"}[l]
}

// Finding — одна находка проверки.
type Finding struct {
	Level Level
	What  string
}

// Verdict — итог проверки узла.
type Verdict struct {
	Findings []Finding
}

// Worst — худший уровень среди находок.
func (v Verdict) Worst() Level {
	w := OK
	for _, f := range v.Findings {
		if f.Level > w {
			w = f.Level
		}
	}
	return w
}

func (v *Verdict) add(l Level, format string, a ...any) {
	v.Findings = append(v.Findings, Finding{l, fmt.Sprintf(format, a...)})
}

// Контейнеры, которые ставят модули vpnkit (не соседи).
var ownContainers = map[string]bool{"mtproxyl": true}

// Минимум RAM (МБ) по ролям, без учёта swap (docs/40-requirements.md).
var minRAM = map[string]int{"exit": 450, "entry": 900, "solo": 900}

// ServerVerdict проверяет сервер под роль.
func ServerVerdict(f Facts, role string) Verdict {
	var v Verdict
	switch {
	case f["os"] == "ubuntu" && (f["os_version"] == "24.04" || f["os_version"] == "26.04"):
		v.add(OK, "ОС %s %s", f["os"], f["os_version"])
	case f["os"] == "debian" && (f["os_version"] == "12" || f["os_version"] == "13"):
		v.add(OK, "ОС %s %s", f["os"], f["os_version"])
	case f["os"] == "ubuntu" || f["os"] == "debian":
		v.add(Warn, "ОС %s %s не проверялась — поддерживаются Ubuntu 24.04/26.04, Debian 12/13", f["os"], f["os_version"])
	default:
		v.add(Fail, "ОС %q %s не поддерживается — нужна Ubuntu или Debian", f["os"], f["os_version"])
	}
	switch f["arch"] {
	case "x86_64", "aarch64":
		v.add(OK, "архитектура %s", f["arch"])
	default:
		v.add(Fail, "архитектура %s не поддерживается (нужна amd64 или arm64)", f["arch"])
	}
	switch virt := f["virt"]; virt {
	case "openvz", "lxc", "lxc-libvirt", "systemd-nspawn", "docker", "podman":
		v.add(Fail, "виртуализация %s: нельзя загрузить модуль ядра AmneziaWG", virt)
	case "unknown", "":
		v.add(Warn, "тип виртуализации не определён — проверьте, что это KVM или выделенный сервер")
	default:
		v.add(OK, "виртуализация %s", virt)
	}
	if f["awg_module"] != "" {
		v.add(OK, "модуль AmneziaWG уже стоит: %s", f["awg_module"])
	} else if h := f["headers"]; h == "" || h == "(none)" {
		v.add(Fail, "нет пакета linux-headers-%s — DKMS-модуль AmneziaWG не собрать", f["kernel"])
	} else {
		v.add(OK, "заголовки ядра доступны (%s)", h)
	}
	if strings.Contains(f["secureboot"], "enabled") {
		v.add(Warn, "Secure Boot включён — модуль DKMS потребует подписи (MOK)")
	}
	mem, swap := f.Int("mem_mb"), f.Int("swap_mb")
	if need := minRAM[role]; mem < need {
		v.add(Fail, "RAM %d МБ, для роли %s нужно не меньше %d МБ", mem, role, need+74)
	} else {
		v.add(OK, "RAM %d МБ, swap %d МБ", mem, swap)
	}
	if mem < 2000 && swap < 1000 {
		v.add(Warn, "мало swap (%d МБ) при RAM %d МБ — установщик предложит swap-файл 2 ГБ", swap, mem)
	}
	needDisk := 5000
	if role != "exit" {
		needDisk = 10000
	}
	if d := f.Int("disk_free_mb"); d < needDisk/2 {
		v.add(Fail, "свободно на диске %d МБ, нужно не меньше %d", d, needDisk/2)
	} else if d < needDisk {
		v.add(Warn, "свободно на диске %d МБ, рекомендуется %d", d, needDisk)
	} else {
		v.add(OK, "свободно на диске %d МБ", d)
	}
	if pub := f["public_ip"]; pub == "" {
		v.add(Warn, "не удалось узнать внешний адрес (нет выхода в интернет?)")
	} else if !strings.Contains(" "+f["if_addrs"]+" ", " "+pub+" ") {
		v.add(Warn, "внешний адрес %s не висит на интерфейсе (%s) — сервер за NAT, входы могут быть недоступны", pub, f["if_addrs"])
	} else {
		v.add(OK, "публичный IPv4 %s на интерфейсе", pub)
	}
	if f["reboot_required"] == "yes" {
		v.add(Warn, "ждёт перезагрузки на новое ядро (vpnkit reboot проверит DKMS)")
	}
	if f["flush_ruleset"] != "" && f["flush_ruleset"] != "0" {
		v.add(Warn, "в /etc/nftables.conf стоит flush ruleset — перезапуск nftables сотрёт чужие таблицы (P-01)")
	}
	var foreign []string
	for _, c := range strings.Fields(f["containers"]) {
		if !ownContainers[c] {
			foreign = append(foreign, c)
		}
	}
	if len(foreign) > 0 {
		v.add(Warn, "чужие контейнеры: %s — соседи, установщик их не трогает", strings.Join(foreign, " "))
	}
	if p := f["p443"]; p != "" && p != "nginx" {
		v.add(Warn, "TCP 443 занят процессом %s — модулям nginx-tls/mtproxy он нужен самим", p)
	}
	return v
}
