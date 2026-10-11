// vpnkit — пульт развёртывания и обслуживания VPN-системы.
// Устройство: docs/ARCHITECTURE.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/xDarkOne/vpnkit/internal/alerts"
	"github.com/xDarkOne/vpnkit/internal/backup"
	"github.com/xDarkOne/vpnkit/internal/bootstrap"
	"github.com/xDarkOne/vpnkit/internal/clients"
	"github.com/xDarkOne/vpnkit/internal/cloudflare"
	"github.com/xDarkOne/vpnkit/internal/health"
	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/modexec"
	"github.com/xDarkOne/vpnkit/internal/probe"
	"github.com/xDarkOne/vpnkit/internal/profile"
	"github.com/xDarkOne/vpnkit/internal/reach"
	"github.com/xDarkOne/vpnkit/internal/remote"
	"github.com/xDarkOne/vpnkit/internal/secrets"
	"github.com/xDarkOne/vpnkit/internal/selfupdate"
	"github.com/xDarkOne/vpnkit/internal/status"
	"github.com/xDarkOne/vpnkit/internal/wizard"
	"github.com/xDarkOne/vpnkit/modules"
	"golang.org/x/term"
)

var version = "0.1.0-dev"

// updateRepo — откуда брать обновления (владелец/репо на GitHub); задаёт сборка релиза:
// -ldflags "-X main.updateRepo=…". Пусто — обновлений нет.
var updateRepo string

// invPathGlobal — путь inventory для команд, которым нужно хранилище секретов рядом с ним.
var invPathGlobal string

const usage = `vpnkit — развёртывание и обслуживание своего VPN: каскад из двух серверов или один сервер

Использование:
  vpnkit [-i inventory.yaml] <команда> [аргументы]

Первая установка (порядок: requirements → inventory → secrets init --key → bootstrap → deploy):
  requirements [роль]   системные требования: entry | exit | solo
  catalog [cascade|solo] что можно поставить в схеме: группы, умолчания, пояснения
  precheck <IP> <роль> [--via <узел>]  новый сервер до добавления: доступность отсюда и проверка по паролю root
  reach <узел|all>      доступен ли узел с этого компьютера (для входа — из России без VPN, P-57)
  dns <имя> <IPv4>      смотрит ли имя на сервер у публичных резолверов
  check <узел>          проверить машину: подходит ли под роль, что мешает
  secrets init [--key]  создать хранилище secrets.age: по паролю или ключом пульта
                        (--key: ~/.vpnkit/age.key, без пароля, как SSH-ключ)
  bootstrap <узел>      первый вход: пароль root → ключ пульта, пароли отключаются
                        (пароль — из ssh.password_env или спросит; не сохраняется)
  deploy [--yes]        всё по схеме inventory (каскад или один сервер): ставит то, чего не хватает (повтор продолжит с места сбоя)
  install <узел> <модуль…> [--yes]  установить модули (снимок, «мёртвая рука», проверка, откат)

Клиенты:
  clients add <узел> <имя> [--iface awg2]  новый клиент: профиль, ссылка vpn://, QR
  clients show|del <узел> <имя> | clients list <узел>  (AWG; del — без перезапуска входа)
  clients add|show|del <узел> <имя> --sub  клиент подписки (hysteria2, XHTTP, PQ): ссылка и QR
  clients list <узел> --sub
  clients add|show|del <узел> <имя> --tg   пользователь Telegram-прокси (MTProxy): ссылка и QR
  clients list <узел> --tg
  clients resync <узел> [--iface awg2]  вернуть пиры AWG из хранилища (сервер заменён, конфиг потерян)

Обслуживание:
  status <узел|all>     сводка: система, модули установщика, итог проверок мониторинга
  doctor <узел|all>     проверки здоровья всех модулей узла
  update [--yes]        обновить модули, чья версия на узлах отстала от пульта (с откатом по шагам)
  profile diff [узлы]   версии компонентов: эталон (пины), узлы, свежие релизы у авторов
  alerts set|chats|test [узел]  оповещения в Telegram (запросы идут с узла: из РФ Telegram API часто закрыт)
  backup pull <узел|all>  забрать свежую копию узла в ~/.vpnkit/backups/<узел>/ и проверить
  backup verify <файл> [роль]  расшифровать ключом пульта и проверить содержимое
  backup extract <файл> <каталог>  распаковать копию (для восстановления)
  restore clients <узел> <копия>        вернуть клиентов подписки и Telegram из резервной копии
  run <узел> '<команда>'  выполнить команду на узле (диагностика, учитывает ssh.jump)

Хранилище секретов:
  secrets pubkey        публичная часть ключа пульта (для шифрования бэкапов узлами)
  secrets set <ключ>    записать секрет (ключ вида узел/модуль/имя, значение спросит)
  secrets list          имена секретов (без значений)
  secrets del <ключ>    удалить секрет

Справка:
  modules [роль]        модули, их ресурсы, порты и зависимости
  version               версия пульта
  self-update [--check] обновить сам vpnkit до последнего релиза (--check — только проверить)

Узлы описываются в inventory.yaml. Примеры: inventory.solo.example.yaml (один сервер),
inventory.cascade.example.yaml (каскад). Подробно — docs/USAGE.md.
Пароль хранилища можно передать через VPNKIT_SECRETS_PASS.
Пароль SSH (если узел без ключа) — из переменной окружения ssh.password_env
или спрашивается.
`

func main() {
	selfupdate.Cleanup(selfupdate.CLI) // прежняя версия после self-update
	fs := flag.NewFlagSet("vpnkit", flag.ExitOnError)
	invPath := fs.String("i", "inventory.yaml", "путь к inventory")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	_ = fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	all, err := modules.Load()
	if err != nil {
		die("манифесты модулей: %v", err)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		fmt.Println("vpnkit", version)
	case "self-update":
		selfUpdateCmd(ctx, first(rest) == "--check")
	case "requirements", "req":
		requirements(all, first(rest))
	case "modules":
		listModules(all, first(rest))
	case "catalog":
		catalogCmd(first(rest))
	case "alerts":
		alertsCmd(ctx, *invPath, rest)
	case "restore":
		if len(rest) != 3 || rest[0] != "clients" {
			die("restore clients <узел> <копия.tar.gz.age>")
		}
		n, err := mustInv(*invPath).Node(rest[1])
		if err != nil {
			die("%v", err)
		}
		data, err := backup.ReadFile(rest[2], "etc/vpnkit/users.json")
		if err != nil {
			die("%v", err)
		}
		if err := clients.RestoreRegistry(ctx, n, data, func(s string) { fmt.Println("  •", s) }); err != nil {
			die("%v", err)
		}
		fmt.Println("  ✓ клиенты подписки и Telegram восстановлены — их ссылки снова работают")
	case "precheck":
		precheckCmd(ctx, *invPath, rest)
	case "deploy":
		invPathGlobal = *invPath
		deployCmd(ctx, mustInv(*invPath), rest)
	case "update":
		invPathGlobal = *invPath
		updateCmd(ctx, mustInv(*invPath), rest)
	case "profile":
		if first(rest) != "diff" {
			die("profile diff [узлы…|all]  — версии: эталон, узлы, свежие релизы")
		}
		inv := mustInv(*invPath)
		args := rest[1:]
		if len(args) == 0 {
			args = []string{"all"}
		}
		ns := pickNodes(inv, args)
		rows, errs := profile.Diff(ctx, ns)
		for name, e := range errs {
			fmt.Printf("  ! %s: %v\n", name, e)
		}
		fmt.Printf("%-11s %-14s", "компонент", "эталон")
		for _, n := range ns {
			fmt.Printf(" %-14s", n.Name)
		}
		fmt.Printf(" %-14s\n", "свежий")
		for _, r := range rows {
			fmt.Printf("%-11s %-14s", r.Name, r.Ref)
			for _, n := range ns {
				v := r.Nodes[n.Name]
				if v == "" {
					v = "—"
				}
				fmt.Printf(" %-14s", v)
			}
			fmt.Printf(" %-14s\n            → %s\n", r.Latest, r.Note)
		}
	case "reach":
		inv := mustInv(*invPath)
		var own []string
		for _, name := range inv.NodeNames() {
			x, _ := inv.Node(name)
			own = append(own, x.Host)
		}
		for _, n := range pickNodes(inv, rest) {
			printReach(n, reach.SSH(ctx, n.Host, n.SSH.Port, own))
		}
	case "dns-create":
		if len(rest) < 2 {
			die("dns-create <домен> <имя=IPv4>…  (токен Cloudflare — в VPNKIT_CF_TOKEN; только создаёт недостающее)")
		}
		want := map[string]string{}
		for _, kv := range rest[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || net.ParseIP(v) == nil {
				die("%q: нужно имя=IPv4", kv)
			}
			want[k] = v
		}
		recs, err := cloudflare.Ensure(ctx, os.Getenv("VPNKIT_CF_TOKEN"), rest[0], want)
		for _, r := range recs {
			fmt.Printf("  %-9s %s → %s %s\n", map[string]string{"created": "создана", "exists": "уже есть", "conflict": "КОНФЛИКТ"}[r.Status], r.Name, r.Want, r.Problem)
		}
		if err != nil {
			die("%v", err)
		}
	case "dns":
		if len(rest) != 2 {
			die("dns <имя> <ожидаемый IPv4>")
		}
		printDNS(reach.DNS(ctx, rest[0], rest[1]))
	case "status":
		inv := mustInv(*invPath)
		for _, n := range pickNodes(inv, rest) {
			printStatus(status.Get(ctx, n))
		}
	case "check":
		inv := mustInv(*invPath)
		for _, n := range pickNodes(inv, rest) {
			check(ctx, n)
		}
	case "doctor":
		inv := mustInv(*invPath)
		sec := openSecrets(*invPath)
		bad := 0
		for _, n := range pickNodes(inv, rest) {
			bad += doctor(ctx, inv, n, all, sec)
		}
		if bad > 0 {
			os.Exit(1)
		}
	case "secrets":
		secretsCmd(*invPath, rest)
	case "clients":
		clientsCmd(ctx, *invPath, rest)
	case "run":
		runCmd(ctx, mustInv(*invPath), rest)
	case "install":
		invPathGlobal = *invPath
		inv := mustInv(*invPath)
		installCmd(ctx, inv, rest)
	case "bootstrap":
		inv := mustInv(*invPath)
		for _, n := range pickNodes(inv, rest) {
			bootstrapCmd(ctx, n)
		}
	case "backup":
		backupCmd(ctx, *invPath, rest)
	case "help", "-h", "--help":
		fs.Usage()
	default:
		fs.Usage()
		die("неизвестная команда %q", cmd)
	}
}

func first(a []string) string {
	if len(a) > 0 {
		return a[0]
	}
	return ""
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "vpnkit: "+f+"\n", a...)
	os.Exit(1)
}

func mustInv(path string) *inventory.Inventory {
	inv, err := inventory.Load(path)
	if err != nil {
		die("%v", err)
	}
	return inv
}

func pickNodes(inv *inventory.Inventory, args []string) []*inventory.Node {
	if len(args) == 0 {
		die("укажите узел: %s или all", strings.Join(inv.NodeNames(), ", "))
	}
	if args[0] == "all" {
		var out []*inventory.Node
		for _, name := range inv.NodeNames() {
			out = append(out, inv.Nodes[name])
		}
		return out
	}
	var out []*inventory.Node
	for _, a := range args {
		n, err := inv.Node(a)
		if err != nil {
			die("%v", err)
		}
		out = append(out, n)
	}
	return out
}

// --- requirements ----------------------------------------------------------

type roleReq struct{ min, rec, note string }

// Сводка docs/40-requirements.md; подробности — там.
var roleReqs = map[string]roleReq{
	"exit":  {"1 vCPU, 512 МБ RAM + 1 ГБ swap, 5 ГБ диска", "1 vCPU, 1 ГБ RAM, 10 ГБ", "за границей; проверить, какой страной адрес видит Google"},
	"entry": {"1 vCPU, 1 ГБ RAM + 2 ГБ swap, 10 ГБ диска", "2 vCPU, 2 ГБ RAM, 20 ГБ", "в России, у провайдера, которого не режут мобильные операторы"},
	"solo":  {"1 vCPU, 1 ГБ RAM + 2 ГБ swap, 10 ГБ диска", "2 vCPU, 2 ГБ RAM, 20 ГБ", "один зарубежный сервер: адрес для клиентов зарубежный, режут первым; нет обхода белых списков"},
}

func requirements(all map[string]modules.Module, role string) {
	roles := []string{"entry", "exit", "solo"}
	if role != "" {
		if _, ok := roleReqs[role]; !ok {
			die("роль %q: допустимы entry, exit, solo", role)
		}
		roles = []string{role}
	}
	fmt.Println("Платформа серверов: Ubuntu 24.04/26.04 или Debian 12/13, amd64/arm64,")
	fmt.Println("KVM или выделенный сервер (OpenVZ/LXC — нет: нужен свой модуль ядра),")
	fmt.Println("пакет linux-headers под текущее ядро, публичный IPv4, root по SSH.")
	fmt.Println("Домены: A-записи без проксирования (DNS only). Подробно — docs/40-requirements.md")
	for _, r := range roles {
		q := roleReqs[r]
		fmt.Printf("\n== %s\n  минимум:       %s\n  рекомендуется: %s\n  важно:         %s\n", r, q.min, q.rec, q.note)
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "  модуль\tRAM, МБ\tдиск/флеш, МБ\tзависит от")
		for _, name := range modules.Names(all) {
			m := all[name]
			if !m.HasRole(r) {
				continue
			}
			disk := m.Resource("disk_mb")
			if disk == "" {
				disk = m.Resource("flash_mb")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", name, m.Resource("ram_mb"), disk, strings.Join(m.Requires, ", "))
		}
		tw.Flush()
	}
}

func listModules(all map[string]modules.Module, role string) {
	for _, name := range modules.Names(all) {
		m := all[name]
		if role != "" && !m.HasRole(role) {
			continue
		}
		fmt.Printf("%s  [%s]  %s\n", name, strings.Join(m.Roles, ","), m.Doc)
		if len(m.Requires) > 0 {
			fmt.Printf("    зависит от: %s\n", strings.Join(m.Requires, ", "))
		}
		var ext []string
		for _, p := range m.Ports {
			if p.Expose {
				ext = append(ext, fmt.Sprintf("%s/%v (%s)", p.Proto, p.Port, p.Purpose))
			}
		}
		if len(ext) > 0 {
			fmt.Printf("    наружу: %s\n", strings.Join(ext, "; "))
		}
		if len(m.Pitfalls) > 0 {
			fmt.Printf("    ловушки: %s\n", strings.Join(m.Pitfalls, " "))
		}
	}
}

// catalogCmd — что можно поставить в схеме: группы, умолчания, пояснения.
func catalogCmd(topo string) {
	if topo == "" {
		topo = modules.Cascade
	}
	roles, ok := modules.Topologies[topo]
	if !ok {
		die("схема %q: допустимы cascade (вход в России + выход за границей) и solo (один сервер)", topo)
	}
	fmt.Printf("Схема %s, узлы: %s. Выбор — в inventory: modules.<имя>.enabled: true|false\n", topo, strings.Join(roles, ", "))
	for _, g := range modules.Groups {
		var ms []modules.Module
		for _, m := range modules.Offered() {
			if m.Catalog.Group == g.ID && len(m.Catalog.Place[topo]) > 0 {
				ms = append(ms, m)
			}
		}
		if len(ms) == 0 {
			continue
		}
		fmt.Printf("\n%s — %s\n", g.Title, g.Note)
		for _, m := range ms {
			c := m.Catalog
			mark := "[ ]"
			switch {
			case c.Required:
				mark = "[■]"
			case c.Auto:
				mark = "[·]"
			case c.Default:
				mark = "[x]"
			}
			fmt.Printf("  %s %-14s %s — %s\n", mark, m.Name, c.Title, c.What)
			fmt.Printf("        %s\n", c.Why)
			var notes []string
			if c.Domain {
				notes = append(notes, "нужен домен")
			}
			if c.Cost != "" {
				notes = append(notes, c.Cost)
			}
			notes = append(notes, "на: "+strings.Join(c.Place[topo], ", "))
			fmt.Printf("        (%s)\n", strings.Join(notes, "; "))
		}
	}
	fmt.Println("\n[■] обязателен  [x] включён по умолчанию  [ ] по желанию  [·] ставится сам, когда нужен другим")
}

// --- check -------------------------------------------------------------------

func check(ctx context.Context, n *inventory.Node) {
	fmt.Printf("== %s (%s, роль %s)\n", n.Name, n.Host, n.Role)
	cl, err := remote.Dial(n)
	if err != nil {
		fmt.Printf("  НЕ ПОДКЛЮЧИЛСЯ: %v\n", err)
		return
	}
	defer cl.Close()
	f, err := probe.Collect(ctx, cl)
	if err != nil {
		fmt.Printf("  сбор фактов не удался: %v\n", err)
		return
	}
	v := probe.ServerVerdict(f, n.Role)
	mark := map[probe.Level]string{probe.OK: "  ✓", probe.Warn: "  !", probe.Fail: "  ✗"}
	for _, x := range v.Findings {
		fmt.Printf("%s %s\n", mark[x.Level], x.What)
	}
	switch v.Worst() {
	case probe.OK:
		fmt.Println("  итог: подходит")
	case probe.Warn:
		fmt.Println("  итог: подходит, но есть замечания")
	default:
		fmt.Println("  итог: НЕ ПОДХОДИТ")
	}
	fmt.Println()
}

// --- doctor ------------------------------------------------------------------

func doctor(ctx context.Context, inv *inventory.Inventory, n *inventory.Node, all map[string]modules.Module, sec *secrets.Store) int {
	fmt.Printf("== %s (%s, роль %s)\n", n.Name, n.Host, n.Role)
	cl, err := remote.Dial(n)
	if err != nil {
		fmt.Printf("  НЕ ПОДКЛЮЧИЛСЯ: %v\n\n", err)
		return 1
	}
	defer cl.Close()
	checks := health.Run(ctx, inv, n, cl, all, sec)
	counts := map[health.Status]int{}
	mark := map[health.Status]string{health.Pass: "✓", health.Look: "?", health.Skip: "–", health.Bad: "✗"}
	for _, c := range checks {
		counts[c.Status]++
		line := fmt.Sprintf("  %s %-15s %s", mark[c.Status], c.Module, short(c.Cmd, 70))
		fmt.Println(line)
		if c.Status != health.Pass {
			if c.Output != "" {
				fmt.Printf("      вывод: %s\n", short(strings.ReplaceAll(c.Output, "\n", " | "), 110))
			}
			if c.Expect != "" {
				fmt.Printf("      ждали: %s\n", c.Expect)
			}
			if c.Note != "" {
				fmt.Printf("      %s\n", c.Note)
			}
		}
	}
	var parts []string
	for _, s := range []health.Status{health.Pass, health.Look, health.Skip, health.Bad} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", s, counts[s]))
		}
	}
	sort.Strings(parts)
	fmt.Printf("  итог: %s\n\n", strings.Join(parts, ", "))
	return counts[health.Bad]
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// --- secrets -----------------------------------------------------------------

func openSecrets(invPath string) *secrets.Store {
	s, err := secrets.Open(secrets.DefaultPath(invPath))
	if err != nil {
		die("%v", err)
	}
	return s
}

func secretsCmd(invPath string, args []string) {
	path := secrets.DefaultPath(invPath)
	switch first(args) {
	case "init":
		if len(args) > 1 && args[1] == "--key" {
			id, err := secrets.LoadKey()
			if err != nil {
				die("%v", err)
			}
			if id == nil {
				if id, err = secrets.CreateKey(); err != nil {
					die("%v", err)
				}
				fmt.Println("создан ключ пульта:", secrets.KeyPath(), "(права 600; держите в резервной копии — без него не расшифровать ни хранилище, ни бэкапы)")
			}
			if _, err := secrets.CreateWithKey(path, id); err != nil {
				die("%v", err)
			}
			fmt.Println("создано:", path, "(зашифровано ключом пульта; в git не добавляется)")
			return
		}
		if _, err := secrets.Create(path); err != nil {
			die("%v", err)
		}
		fmt.Println("создано:", path, "(держите в секрете и в резервной копии; в git не добавляется)")
	case "pubkey":
		id, err := secrets.LoadKey()
		if err != nil || id == nil {
			die("ключа пульта нет — vpnkit secrets init --key")
		}
		fmt.Println(id.Recipient().String())
	case "set":
		if len(args) < 2 {
			die("укажите ключ: vpnkit secrets set узел/модуль/имя")
		}
		if !secrets.Exists(path) {
			die("хранилища нет — сначала vpnkit secrets init")
		}
		s := openSecrets(invPath)
		v, err := readSecretValue(args[1])
		if err != nil {
			die("%v", err)
		}
		if err := s.Set(args[1], v); err != nil {
			die("%v", err)
		}
		if err := s.Save(); err != nil {
			die("%v", err)
		}
		fmt.Println("записано:", args[1])
	case "list":
		for _, k := range openSecrets(invPath).Keys() {
			fmt.Println(k)
		}
	case "del":
		if len(args) < 2 {
			die("укажите ключ")
		}
		s := openSecrets(invPath)
		if !s.Delete(args[1]) {
			die("ключа %s нет", args[1])
		}
		if err := s.Save(); err != nil {
			die("%v", err)
		}
		fmt.Println("удалено:", args[1])
	default:
		die("secrets: init [--key] | set <ключ> | list | del <ключ> | pubkey")
	}
}

// readSecretValue читает значение из VPNKIT_SECRET_VALUE или с терминала без эха.
func readSecretValue(key string) (string, error) {
	if v := os.Getenv("VPNKIT_SECRET_VALUE"); v != "" {
		return v, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("нет терминала — передайте значение в VPNKIT_SECRET_VALUE")
	}
	fmt.Fprintf(os.Stderr, "Значение %s: ", key)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// --- adopt -------------------------------------------------------------------

// --- backup ------------------------------------------------------------------

func backupCmd(ctx context.Context, invPath string, args []string) {
	switch first(args) {
	case "pull":
		inv := mustInv(invPath)
		for _, n := range pickNodes(inv, args[1:]) {
			fmt.Printf("== %s\n", n.Name)
			cl, err := remote.Dial(n)
			if err != nil {
				fmt.Printf("  НЕ ПОДКЛЮЧИЛСЯ: %v\n", err)
				continue
			}
			p, err := backup.Pull(ctx, cl, filepath.Join(remote.StateDir(), "backups", n.Name))
			cl.Close()
			if err != nil {
				fmt.Printf("  ✗ %v\n", err)
				continue
			}
			fmt.Printf("  ✓ %s (sha256 сверен)\n", p)
			printVerify(p, n.Role)
		}
	case "verify":
		if len(args) < 2 {
			die("backup verify <файл> [entry|exit]")
		}
		role := "entry"
		if len(args) > 2 {
			role = args[2]
		}
		printVerify(args[1], role)
	case "extract":
		if len(args) < 3 {
			die("backup extract <файл> <каталог>")
		}
		n, err := backup.Extract(args[1], args[2])
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("распаковано файлов: %d в %s (там секреты — удалите после восстановления)\n", n, args[2])
	default:
		die("backup: pull <узел|all> | verify <файл> [роль] | extract <файл> <каталог>")
	}
}

func printVerify(path, role string) {
	rep, err := backup.Verify(path, role)
	if err != nil {
		fmt.Printf("  ✗ проверка: %v\n", err)
		return
	}
	fmt.Printf("  ✓ расшифровано ключом пульта: %d файлов, %.1f МБ\n", rep.Files, float64(rep.Bytes)/1e6)
	var tops []string
	for k, v := range rep.Top {
		tops = append(tops, fmt.Sprintf("%s(%d)", k, v))
	}
	sort.Strings(tops)
	fmt.Printf("    %s\n", strings.Join(tops, " "))
	if len(rep.Missing) > 0 {
		fmt.Printf("  ✗ НЕТ ключевых файлов: %s\n", strings.Join(rep.Missing, ", "))
	} else if len(rep.Modules) > 0 {
		fmt.Printf("  ✓ все ключевые файлы модулей на месте: %s\n", strings.Join(rep.Modules, ", "))
	} else {
		fmt.Printf("  ✓ все ключевые файлы роли %s на месте\n", role)
	}
}

// --- bootstrap ---------------------------------------------------------------

func bootstrapCmd(ctx context.Context, n *inventory.Node) {
	fmt.Printf("== %s (%s)\n", n.Name, n.Host)
	pw := ""
	if n.SSH.PasswordEnv != "" {
		pw = os.Getenv(n.SSH.PasswordEnv)
	}
	if pw == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			die("нет пароля: задайте переменную %s", n.SSH.PasswordEnv)
		}
		fmt.Fprintf(os.Stderr, "Пароль root %s: ", n.Host)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			die("%v", err)
		}
		pw = string(b)
	}
	err := bootstrap.Run(ctx, n, pw, func(s string) { fmt.Println("  •", s) })
	if err != nil {
		fmt.Printf("  ✗ %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ узел переведён на ключ; в inventory оставьте ssh.auth: key")
}

// --- install -----------------------------------------------------------------

func installCmd(ctx context.Context, inv *inventory.Inventory, args []string) {
	yes := false
	var rest []string
	for _, a := range args {
		if a == "--yes" {
			yes = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) < 2 {
		die("install <узел> <модуль…> [--yes]")
	}
	n, err := inv.Node(rest[0])
	if err != nil {
		die("%v", err)
	}
	mods := rest[1:]
	sec := openSecrets(invPathGlobal)
	fmt.Printf("План для %s (%s, роль %s):\n", n.Name, n.Host, n.Role)
	for _, m := range mods {
		var ps []string
		for k, v := range modexec.Params(n, m) {
			ps = append(ps, k+"="+v)
		}
		sort.Strings(ps)
		fmt.Printf("  • %s  [%s]\n", m, strings.Join(ps, " "))
	}
	fmt.Println("  каждый модуль: снимок → откат по таймеру (свой срок у модуля) → установка → проверка → новое подключение")
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			die("подтвердите: добавьте --yes")
		}
		fmt.Print("Продолжить? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			die("отменено")
		}
	}
	for _, m := range mods {
		fmt.Printf("== %s: %s\n", n.Name, m)
		if err := modexec.Install(ctx, inv, n, m, sec, func(s string) { fmt.Println(s) }); err != nil {
			fmt.Printf("  ✗ %s: %v\n", m, err)
			os.Exit(1)
		}
		fmt.Printf("  ✓ %s установлен\n", m)
	}
}

// --- run ---------------------------------------------------------------------

// runCmd выполняет команду на узле (с учётом ssh.jump) и печатает вывод.
// Служебная команда для диагностики; изменения на узлах делать не ей, а модулями.
func runCmd(ctx context.Context, inv *inventory.Inventory, args []string) {
	if len(args) < 2 {
		die("run <узел> '<команда>'")
	}
	n, err := inv.Node(args[0])
	if err != nil {
		die("%v", err)
	}
	cl, err := remote.Dial(n)
	if err != nil {
		die("%v", err)
	}
	defer cl.Close()
	r, err := cl.Run(ctx, strings.Join(args[1:], " ")+"\n")
	fmt.Print(r.Stdout)
	fmt.Fprint(os.Stderr, r.Stderr)
	if err != nil {
		die("%v", err)
	}
	os.Exit(r.Code)
}

// --- clients -----------------------------------------------------------------

func clientsCmd(ctx context.Context, invPath string, args []string) {
	inv := mustInv(invPath)
	sec := openSecrets(invPath)
	iface := ""
	sub, tg := false, false
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--iface" && i+1 < len(args) {
			iface = args[i+1]
			i++
			continue
		}
		if args[i] == "--sub" {
			sub = true
			continue
		}
		if args[i] == "--tg" {
			tg = true
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) < 2 {
		die("clients add|show|del <узел> <имя> [--iface awg2 | --sub | --tg] | clients list <узел> [--sub | --tg]")
	}
	n, err := inv.Node(rest[1])
	if err != nil {
		die("%v", err)
	}
	if sub {
		subClientsCmd(ctx, n, rest)
		return
	}
	if tg {
		tgClientsCmd(ctx, n, rest)
		return
	}
	switch rest[0] {
	case "resync":
		added, extra, err := clients.Resync(ctx, n, iface, sec, func(s string) { fmt.Println("  •", s) })
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("  ✓ возвращено пиров: %d\n", len(added))
		if len(extra) > 0 {
			fmt.Printf("  ! на узле есть пиры, которых нет в хранилище (не тронуты): %s\n", strings.Join(extra, ", "))
		}
		return
	case "list":
		for _, c := range clients.Names(n, sec) {
			fmt.Println(c)
		}
		return
	case "add", "show":
		if len(rest) < 3 {
			die("укажите имя клиента")
		}
		var p clients.Profile
		if rest[0] == "add" {
			p, err = clients.Add(ctx, n, iface, rest[2], sec, func(s string) { fmt.Println("  •", s) })
		} else {
			p, err = clients.Show(n, iface, rest[2], sec)
		}
		if err != nil {
			die("%v", err)
		}
		saveClientFiles(n, p)
	case "del":
		if len(rest) < 3 {
			die("укажите имя клиента")
		}
		if err := clients.Del(ctx, n, iface, rest[2], sec, func(s string) { fmt.Println("  •", s) }); err != nil {
			die("%v", err)
		}
		clients.RemoveFiles(n.Name, "awg", rest[2])
		fmt.Printf("  ✓ клиент %s удалён: его профиль и ссылка больше не работают\n", rest[2])
	default:
		die("clients add|show|del|list")
	}
}

// tgClientsCmd — пользователи MTProxy (telemt): реестр на узле, ссылка и QR — на пульте.
func tgClientsCmd(ctx context.Context, n *inventory.Node, rest []string) {
	name := ""
	if len(rest) > 2 {
		name = rest[2]
	} else if rest[0] != "list" {
		die("укажите имя пользователя")
	}
	switch rest[0] {
	case "add", "show", "del", "list":
	default:
		die("clients add|show|del|list --tg")
	}
	c, list, err := clients.TGClient(ctx, n, rest[0], name)
	if err != nil {
		die("%v", err)
	}
	switch rest[0] {
	case "list":
		for _, x := range list {
			fmt.Println(x)
		}
		return
	case "del":
		clients.RemoveFiles(n.Name, "tg", name)
		fmt.Printf("  ✓ пользователь %s удалён: его ссылка больше не пускает в прокси (без перезапуска прокси)\n", name)
		return
	}
	printCard(clients.TGCardOf(n.Name, c))
}

// subClientsCmd — клиенты подписки (xray/hysteria2): реестр на узле, файлы — на пульте.
func subClientsCmd(ctx context.Context, n *inventory.Node, rest []string) {
	if rest[0] == "list" {
		ns, err := clients.SubNames(ctx, n)
		if err != nil {
			die("%v", err)
		}
		for _, x := range ns {
			fmt.Println(x)
		}
		return
	}
	if len(rest) < 3 {
		die("укажите имя клиента")
	}
	var c clients.SubCard
	var err error
	switch rest[0] {
	case "add":
		c, err = clients.SubAdd(ctx, n, rest[2])
	case "show":
		c, err = clients.SubShow(ctx, n, rest[2])
	case "del":
		if err = clients.SubDel(ctx, n, rest[2]); err == nil {
			clients.RemoveFiles(n.Name, "sub", rest[2])
			fmt.Printf("  ✓ клиент %s удалён: подписка и ключи больше не работают\n", rest[2])
			return
		}
	default:
		die("clients add|show|del|list --sub")
	}
	if err != nil {
		die("%v", err)
	}
	printCard(clients.SubCardOf(n.Name, c))
}

// saveClientFiles — карточка клиента AmneziaWG на пульт.
func saveClientFiles(n *inventory.Node, p clients.Profile) {
	printCard(clients.AWGCard(n.Name, p))
}

// printCard сохраняет файлы карточки в ~/.vpnkit/clients/<узел>/ и говорит, где они.
func printCard(c clients.Card) {
	if err := c.Save(); err != nil {
		die("%v", err)
	}
	fmt.Printf("  ✓ %s: %s\n", c.Name, c.Title)
	switch c.Kind {
	case "awg":
		fmt.Printf("    профиль: %s.conf, ссылка: %s.vpn.txt, QR: %s.qr.png\n", c.Base, c.Base, c.Base)
	case "sub":
		fmt.Printf("    подписка: %s\n    файл: %s.sub.txt, QR: %s.sub.qr.png\n", c.QR, c.Base, c.Base)
	case "tg":
		fmt.Printf("    файл: %s.tg.txt, QR: %s.tg.qr.png\n", c.Base, c.Base)
	}
	fmt.Println("    " + c.Hint)
	fmt.Println("    в файлах — ключ доступа: передавайте только самому человеку")
}

// alertsCmd — оповещения в Telegram. Запросы к Bot API идут с узла (по умолчанию — с выхода):
// из России Telegram API без VPN часто недоступен.
func alertsCmd(ctx context.Context, invPath string, args []string) {
	if len(args) == 0 {
		die("alerts set | chats | test [узел]")
	}
	inv := mustInv(invPath)
	var n *inventory.Node
	if len(args) > 1 {
		var err error
		if n, err = inv.Node(args[1]); err != nil {
			die("%v", err)
		}
	} else {
		for _, name := range inv.NodeNames() {
			x, _ := inv.Node(name)
			if x.Role == "exit" || n == nil {
				n = x
			}
		}
	}
	sec := openSecrets(invPath)
	token, _ := sec.Get(alerts.TokenKey)
	switch args[0] {
	case "set":
		t := os.Getenv("VPNKIT_TG_TOKEN")
		if t == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				die("нужен токен бота: задайте VPNKIT_TG_TOKEN")
			}
			fmt.Fprint(os.Stderr, "Токен бота от @BotFather: ")
			b, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			t = strings.TrimSpace(string(b))
		}
		name, err := alerts.BotName(ctx, n, t)
		if err != nil {
			die("%v", err)
		}
		fmt.Printf("  ✓ бот %s (проверено с %s)\n", name, n.Name)
		chats := os.Getenv("VPNKIT_TG_CHATS")
		if chats == "" {
			list, _ := alerts.RecentChats(ctx, n, t)
			for _, c := range list {
				fmt.Printf("    писал боту: %s — %s\n", c.ID, c.Name)
			}
			fmt.Print("Получатели (chat id через пробел; свой — напишите боту /start и смотрите список выше): ")
			fmt.Scanln(&chats)
		}
		ids, err := alerts.ParseChats(chats)
		if err != nil {
			die("%v", err)
		}
		if err := alerts.Send(ctx, n, t, ids, "vpnkit: оповещения подключены ✓"); err != nil {
			die("тестовое сообщение не ушло: %v", err)
		}
		must := func(err error) {
			if err != nil {
				die("%v", err)
			}
		}
		must(sec.Set(alerts.TokenKey, t))
		must(sec.Set(alerts.ChatsKey, strings.Join(ids, " ")))
		must(sec.Save())
		fmt.Println("  ✓ тестовое сообщение отправлено, токен сохранён в хранилище")
		fmt.Println("    чтобы узлы начали слать оповещения: vpnkit install <узел> monitoring (на каждом)")
	case "chats":
		if token == "" {
			die("токена нет: vpnkit alerts set")
		}
		list, err := alerts.RecentChats(ctx, n, token)
		if err != nil {
			die("%v", err)
		}
		if len(list) == 0 {
			fmt.Println("  боту никто не писал за последние сутки — напишите ему /start и повторите")
		}
		for _, c := range list {
			fmt.Printf("  %s — %s\n", c.ID, c.Name)
		}
	case "test":
		chats, _ := sec.Get(alerts.ChatsKey)
		if token == "" || chats == "" {
			die("оповещения не настроены: vpnkit alerts set")
		}
		if err := alerts.Send(ctx, n, token, strings.Fields(chats), "vpnkit: проверка оповещений с "+n.Name); err != nil {
			die("%v", err)
		}
		fmt.Println("  ✓ отправлено")
	default:
		die("alerts set | chats | test [узел]")
	}
}

// precheckCmd — новый сервер до добавления: доступность отсюда и проверка по паролю root
// (ничего не меняет). --via <узел> — через уже настроенный узел, если сервер отсюда не виден.
func precheckCmd(ctx context.Context, invPath string, args []string) {
	var rest []string
	via := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--via" && i+1 < len(args) {
			via = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) != 2 {
		die("precheck <IP> <entry|exit|solo> [--via <узел>]")
	}
	n := &inventory.Node{Name: rest[0], Host: rest[0], Role: rest[1], SSH: inventory.SSH{User: "root", Port: 22}}
	v := reach.SSH(ctx, n.Host, 22, nil)
	printReach(n, v)
	if via != "" {
		j, err := mustInv(invPath).Node(via)
		if err != nil {
			die("%v", err)
		}
		n.SSH.Jump = &inventory.Jump{Host: j.Host, Port: j.SSH.Port, User: j.SSH.User, Key: j.SSH.Key}
		fmt.Printf("  подключаюсь через %s\n", j.Name)
	} else if !v.OK {
		die("сервер отсюда недоступен; если это выход — проверьте через вход: --via <вход>")
	}
	pw := os.Getenv("VPNKIT_ROOT_PASS")
	if pw == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			die("нужен пароль root: задайте VPNKIT_ROOT_PASS")
		}
		fmt.Fprint(os.Stderr, "Пароль root (не сохраняется): ")
		b, _ := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		pw = string(b)
	}
	c := wizard.CheckServer(ctx, n, pw)
	if c.Error != "" {
		die("%s", c.Error)
	}
	mark := map[string]string{"ok": "  ✓", "warn": "  !", "fail": "  ✗"}
	for _, f := range c.Findings {
		fmt.Printf("%s %s\n", mark[f.Level], f.What)
	}
	fmt.Println(map[string]string{"ok": "Подходит.", "warn": "Подходит, но посмотрите предупреждения.", "fail": "НЕ ПОДХОДИТ."}[c.Worst])
}

// collectInstalled — что стоит на серверах каскада: узел → модуль → версия кода из метки.
func collectInstalled(ctx context.Context, inv *inventory.Inventory) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		st := status.Get(ctx, n)
		if !st.Reachable || st.Error != "" {
			die("%s: нет связи — %s", n.Name, st.Error)
		}
		out[n.Name] = map[string]string{}
		for _, m := range st.Modules {
			out[n.Name][m.Name] = m.Code
		}
	}
	return out
}

// updateCmd — обновить модули, чья версия на узлах отстала от пульта (скрипты или пины).
func updateCmd(ctx context.Context, inv *inventory.Inventory, args []string) {
	yes := len(args) > 0 && args[0] == "--yes"
	plan, err := wizard.Updates(inv, collectInstalled(ctx, inv))
	if err != nil {
		die("%v", err)
	}
	if len(plan) == 0 {
		fmt.Println("Обновлять нечего: все модули на узлах — версии этого пульта.")
		return
	}
	fmt.Println("Доступны обновления:")
	for _, s := range plan {
		from := s.From
		if from == "" {
			from = "до учёта версий"
		}
		fmt.Printf("  → %-24s %-8s %s → %s\n", s.Title, s.Node, from, s.To)
	}
	fmt.Printf("Шагов: %d. Каждый: снимок → откат по таймеру → установка → проверка; службы перезапускаются, только если их конфиг или бинарник изменились.\n", len(plan))
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			die("подтвердите: update --yes")
		}
		fmt.Print("Продолжить? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			die("отменено")
		}
	}
	sec := openSecrets(invPathGlobal)
	if err := wizard.Run(ctx, inv, sec, plan, func(e wizard.Event) { fmt.Println(e.Line) }); err != nil {
		die("%v\nОбновлённые шаги остаются; повтор update продолжит с упавшего.", err)
	}
	fmt.Println("Все модули обновлены.")
}

// deployCmd — установка целиком по схеме inventory (каскад или один сервер): что уже стоит
// (метки на узлах), план по каталогу модулей, установка недостающего.
func deployCmd(ctx context.Context, inv *inventory.Inventory, args []string) {
	yes := len(args) > 0 && args[0] == "--yes"
	installed := map[string][]string{}
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		st := status.Get(ctx, n)
		if !st.Reachable || st.Error != "" {
			die("%s: нет связи — %s", n.Name, st.Error)
		}
		for _, m := range st.Modules {
			installed[n.Name] = append(installed[n.Name], m.Name)
		}
	}
	plan, err := wizard.Plan(inv, installed)
	if err != nil {
		die("%v", err)
	}
	todo := 0
	fmt.Printf("План (%s):\n", map[string]string{"cascade": "каскад из двух серверов", "solo": "один сервер"}[inv.Topo()])
	for _, s := range plan {
		mark := "→"
		note := s.Reason
		switch {
		case s.Done:
			mark, note = "✓", "стоит"
		case s.Skip:
			mark = "–"
		default:
			todo++
		}
		fmt.Printf("  %s %-32s %-8s %s\n", mark, s.Title, s.Node, note)
	}
	for _, p := range wizard.Problems(plan) {
		die("%s. Включите способ подключения в inventory (modules.<имя>.enabled: true) или задайте домен.", p)
	}
	if todo == 0 {
		fmt.Println("Ставить нечего: всё на месте.")
		return
	}
	fmt.Printf("Шагов к установке: %d. Каждый: снимок → откат по таймеру → установка → проверка.\n", todo)
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			die("подтвердите: deploy --yes")
		}
		fmt.Print("Продолжить? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if strings.ToLower(ans) != "y" {
			die("отменено")
		}
	}
	sec := openSecrets(invPathGlobal)
	if err := wizard.Run(ctx, inv, sec, plan, func(e wizard.Event) { fmt.Println(e.Line) }); err != nil {
		die("%v\nПройденные шаги остаются; после исправления запустите deploy снова — он продолжит с этого места.", err)
	}
	fmt.Println("Каскад готов.")
}

func printReach(n *inventory.Node, v reach.Verdict) {
	fmt.Printf("== %s (%s:%d, %s)\n", n.Name, v.Host, v.Port, n.Role)
	if v.ViaOwn != "" {
		fmt.Printf("  ! этот компьютер выходит в интернет через %s (свой узел) — проба врёт, выключите VPN\n", v.ViaOwn)
	} else if v.MyIP != "" {
		fmt.Printf("  этот компьютер: %s\n", v.MyIP)
	}
	if v.OK {
		fmt.Printf("  ✓ доступен отсюда: %s (%d мс)\n", v.Banner, v.Millis)
		return
	}
	fmt.Printf("  ✗ %s\n", v.Problem)
	if n.Role == "entry" && v.Internet && v.ViaOwn == "" {
		fmt.Println("    вход обязан быть доступен из России без VPN — этот сервер на роль входа не годится (P-57)")
	}
}

func printDNS(r reach.DNSResult) {
	for _, a := range r.Answers {
		got := strings.Join(a.Addrs, ", ")
		if a.Error != "" {
			got = a.Error
		}
		fmt.Printf("  %-10s %s\n", a.Resolver, got)
	}
	if r.OK {
		fmt.Printf("  ✓ %s → %s у всех резолверов\n", r.Name, r.Want)
	} else {
		fmt.Printf("  ✗ %s\n", r.Problem)
	}
}

func printStatus(st status.Status) {
	fmt.Printf("== %s (%s, %s)\n", st.Node, st.Host, st.Role)
	if !st.Reachable || st.Error != "" {
		fmt.Printf("  ✗ нет связи: %s\n", st.Error)
		return
	}
	fmt.Printf("  %s, ядро %s, работает %s\n  диск %s, память %s\n", st.OS, st.Kernel, st.Uptime, st.Disk, st.Mem)
	var ms []string
	for _, m := range st.Modules {
		ms = append(ms, m.Name)
	}
	fmt.Printf("  модули (%d): %s\n", len(ms), strings.Join(ms, ", "))
	switch {
	case !st.Watch:
		fmt.Println("  мониторинг не стоит")
	case len(st.Failed) == 0:
		fmt.Printf("  ✓ проверок %d, все прошли\n", st.Checks)
	default:
		fmt.Printf("  ✗ проверок %d, не прошло %d:\n", st.Checks, len(st.Failed))
		for _, f := range st.Failed {
			fmt.Println("    • " + f)
		}
	}
}

// selfUpdateCmd — новая версия CLI из релизов: скачать, сверить по SHA256SUMS, подменить себя.
func selfUpdateCmd(ctx context.Context, check bool) {
	c := selfupdate.Status(ctx, updateRepo, version, selfupdate.CLI)
	switch {
	case c.Latest == "":
		die("проверить обновления не удалось: %s", c.Reason)
	case !c.Newer:
		fmt.Printf("vpnkit %s — последняя версия\n", version)
		return
	case check:
		fmt.Printf("доступна vpnkit %s (у вас %s): %s\n", c.Latest, version, c.URL)
		return
	case !c.CanApply:
		die("доступна %s, но обновиться на месте нельзя: %s\nСкачайте вручную: %s", c.Latest, c.Reason, c.URL)
	}
	if _, err := selfupdate.Apply(ctx, updateRepo, selfupdate.CLI, func(s string) { fmt.Println("  •", s) }); err != nil {
		die("%v", err)
	}
	fmt.Printf("  ✓ vpnkit обновлён: %s → %s\n", version, c.Latest)
}
