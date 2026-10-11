// Package modexec — установка модуля на узел с «мёртвой рукой».
//
//  1. preflight (если есть);
//  2. на узел кладётся скрипт отката и взводится таймер: через Deadman
//     узел откатит модуль САМ, если пульт не подтвердит успех;
//  3. install → verify; ошибка → откат сразу;
//  4. пульт подключается к узлу ЗАНОВО (новая сессия — проверка, что доступ
//     не потерян, например из-за фаервола) и только тогда снимает таймер.
package modexec

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xDarkOne/vpnkit/agent"
	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/remote"
	"github.com/xDarkOne/vpnkit/internal/secrets"
	"github.com/xDarkOne/vpnkit/modules"
)

// Deadman — срок «мёртвой руки» по умолчанию (модуль может задать свой: deadman_sec).
const Deadman = 180 * time.Second

// deadmanFor — срок для модуля из манифеста или умолчание.
func deadmanFor(module string) time.Duration {
	all, err := modules.Load()
	if err == nil {
		if m, ok := all[module]; ok && m.DeadmanSec > 0 {
			return time.Duration(m.DeadmanSec) * time.Second
		}
	}
	return Deadman
}

// Метки heredoc: обёртка шага и вложенный файл отката ОБЯЗАНЫ различаться —
// иначе внешний heredoc закрывается на внутренней метке (поймано 02.10.2026).
const (
	stepTag     = "VKSTEP_EOF_5e2a"
	rollbackTag = "VKROLLBACK_EOF_9b4c"
)

// Params собирает переменные VK_* для модуля: роль, порт SSH, настройки модуля из inventory.
func Params(n *inventory.Node, module string) map[string]string {
	p := map[string]string{"ROLE": n.Role, "SSH_PORT": fmt.Sprint(n.SSH.Port)}
	for k, v := range n.Modules[module] {
		p[strings.ToUpper(strings.ReplaceAll(k, "-", "_"))] = fmt.Sprint(v)
	}
	return p
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'" }

func envBlock(module, run string, params map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export VK_MODULE=%s VK_RUN=%s\n", shq(module), shq(run))
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "export VK_%s=%s\n", k, shq(params[k]))
	}
	return b.String()
}

// platform — чем узел отличается для исполнителя: оболочка, библиотека, где прогоны и метки,
// как взвести и снять «мёртвую руку».
type platform struct {
	shell  string // bash
	lib    string
	runs   string // каталог прогонов (снимки, откат)
	marks  string // метки установленных модулей
	arm    func(run, unit string, secs int) string
	disarm func(run, unit string) string
}

var server = platform{
	shell: "bash", runs: "/var/lib/vpnkit/runs", marks: "/var/lib/vpnkit/modules",
	arm: func(run, unit string, secs int) string {
		return fmt.Sprintf(`systemctl stop %[1]s.timer 2>/dev/null || true; systemctl reset-failed %[1]s.service 2>/dev/null || true
systemd-run --quiet --unit=%[1]s --on-active=%[2]d /bin/bash %[3]s/rollback.sh`, unit, secs, run)
	},
	disarm: func(run, unit string) string { return fmt.Sprintf("systemctl stop %s.timer 2>/dev/null || true", unit) },
}

func platformOf(n *inventory.Node) platform {
	p := server
	p.lib = agent.Lib()
	return p
}

// script — полный текст шага: окружение + библиотека платформы + шаг.
func (p platform) script(module, run, step string, params map[string]string) (string, bool) {
	body, ok := agent.Step(module, step)
	if !ok {
		return "", false
	}
	return envBlock(module, run, params) + p.lib + "\n# ---- " + module + "/" + step + "\n" + body, true
}

func bash(ctx context.Context, cl *remote.Client, s string) (string, error) {
	return execIn(ctx, cl, "bash", s)
}

func execIn(ctx context.Context, cl *remote.Client, shell, s string) (string, error) {
	if strings.Contains(s, stepTag) {
		return "", fmt.Errorf("в скрипте встречается метка %s", stepTag)
	}
	// в тексте шага бывают секреты (ключи) → временный файл только для root (600) и сразу удаляется
	wrapped := "umask 077; f=$(mktemp /tmp/.vk-step.XXXXXX)\ncat > \"$f\" <<'" + stepTag + "'\n" + s + "\n" + stepTag + "\n" +
		shell + " \"$f\"; rc=$?; rm -f \"$f\"; exit $rc\n"
	r, err := cl.Run(ctx, wrapped)
	out := strings.TrimRight(r.Stdout+r.Stderr, "\n")
	if err != nil {
		return out, err
	}
	if r.Code != 0 {
		return out, fmt.Errorf("код выхода %d", r.Code)
	}
	return out, nil
}

// Install ставит модуль. log — построчный вывод хода работы.
func Install(ctx context.Context, inv *inventory.Inventory, n *inventory.Node, module string, sec *secrets.Store, log func(string)) error {
	if _, ok := agent.Step(module, "install"); !ok {
		return fmt.Errorf("у модуля %s нет шага install (ещё не реализован)", module)
	}
	params, err := Prepare(inv, n, module, sec)
	if err != nil {
		return fmt.Errorf("подготовка параметров: %w", err)
	}
	pf := platformOf(n)
	// пакеты и другие двоичные файлы пульт качает и сверяет сам — до любых изменений на узле
	arts, err := artifacts(ctx, n, module, params, sec)
	if err != nil {
		return fmt.Errorf("подготовка файлов для узла: %w (ничего не менялось)", err)
	}
	run := fmt.Sprintf("%s/%s-%s", pf.runs, module, time.Now().Format("20060102-150405"))
	unit := "vpnkit-deadman-" + module
	dm := deadmanFor(module)
	emit := func(out string) {
		for _, l := range strings.Split(out, "\n") {
			if strings.TrimSpace(l) != "" {
				log(l)
			}
		}
	}
	sh := func(c *remote.Client, s string) (string, error) { return execIn(ctx, c, pf.shell, s) }

	cl, err := remote.Dial(n)
	if err != nil {
		return err
	}
	defer cl.Close()

	if s, ok := pf.script(module, run, "preflight", params); ok {
		out, err := sh(cl, s)
		emit(out)
		if err != nil {
			return fmt.Errorf("preflight: %w (ничего не менялось)", err)
		}
	}

	// откат и «мёртвая рука» — ДО любых изменений
	rb, ok := pf.script(module, run, "rollback", params)
	if !ok {
		return fmt.Errorf("у модуля %s нет шага rollback — ставить без отката нельзя", module)
	}
	arm := fmt.Sprintf(`set -e
mkdir -p %[1]s; chmod 700 %[1]s
cat > %[1]s/rollback.sh <<'%[2]s'
%[3]s
%[2]s
chmod 700 %[1]s/rollback.sh
%[4]s
echo "  • мёртвая рука: откат через %[5]d с, если пульт не подтвердит"
`, run, rollbackTag, rb, pf.arm(run, unit, int(dm.Seconds())), int(dm.Seconds()))
	out, err := sh(cl, arm)
	emit(out)
	if err != nil {
		return fmt.Errorf("не удалось взвести откат: %w (ничего не менялось)", err)
	}

	rollbackNow := func(why error) error {
		log("  ✗ " + why.Error() + " — откатываю")
		out, rerr := sh(cl, pf.disarm(run, unit)+"\n"+pf.shell+" "+run+"/rollback.sh\n")
		emit(out)
		if rerr != nil {
			return fmt.Errorf("%v; ОТКАТ НЕ УДАЛСЯ: %v", why, rerr)
		}
		return fmt.Errorf("%v (откачено)", why)
	}

	// файлы модуля (скрипты, юниты) → $VK_RUN/src/ — шаг install берёт их оттуда
	if fs := agent.Files(module); len(fs) > 0 {
		var up strings.Builder
		fmt.Fprintf(&up, "set -e\numask 077\nmkdir -p %s/src\n", run)
		i := 0
		for name, body := range fs {
			tag := fmt.Sprintf("VKFILE_EOF_%d_3f8a", i)
			if strings.Contains(body, tag) {
				return fmt.Errorf("в файле %s встречается метка %s", name, tag)
			}
			fmt.Fprintf(&up, "cat > %s/src/%s <<'%s'\n%s\n%s\n", run, name, tag, strings.TrimRight(body, "\n"), tag)
			i++
		}
		fmt.Fprintf(&up, "echo \"  • файлов модуля выложено: %d\"\n", len(fs))
		out, err := sh(cl, up.String())
		emit(out)
		if err != nil {
			return rollbackNow(fmt.Errorf("выкладка файлов модуля: %w", err))
		}
	}
	// двоичные файлы — через stdin как есть, с проверкой sha256 на узле
	for _, a := range arts {
		dst := run + "/src/" + a.Name
		r, err := cl.RunStdin(ctx, "umask 077; mkdir -p "+run+"/src && cat > "+dst, string(a.Data))
		if err != nil || r.Code != 0 {
			return rollbackNow(fmt.Errorf("передача %s: %v %s", a.Name, err, r.Stderr))
		}
		out, err := sh(cl, fmt.Sprintf("[ \"$(sha256sum %s | cut -d' ' -f1)\" = %s ] || { echo \"  ✗ %s пришёл повреждённым\" >&2; exit 1; }\n", dst, a.SHA256, a.Name))
		emit(out)
		if err != nil {
			return rollbackNow(fmt.Errorf("передача %s: %w", a.Name, err))
		}
	}
	if len(arts) > 0 {
		log(fmt.Sprintf("  • пакетов передано: %d (sha256 сверен на узле)", len(arts)))
	}

	s, _ := pf.script(module, run, "install", params)
	out, err = sh(cl, s)
	emit(out)
	if err != nil {
		return rollbackNow(fmt.Errorf("install: %w", err))
	}
	if s, ok := pf.script(module, run, "verify", params); ok {
		out, err = sh(cl, s)
		emit(out)
		if err != nil {
			return rollbackNow(fmt.Errorf("verify: %w", err))
		}
	}
	// проверка самого отката: параметр selftest_rollback в inventory — откатить после удачной
	// проверки (стенды; так видно, что откат возвращает узел как был)
	if params["SELFTEST_ROLLBACK"] == "true" || params["SELFTEST_ROLLBACK"] == "1" {
		return rollbackNow(fmt.Errorf("selftest_rollback: проверка отката по запросу"))
	}

	// доступ не потерян? — новое подключение
	cl2, err := remote.Dial(n)
	if err != nil {
		return fmt.Errorf("после установки новое подключение не удалось (%v): узел откатит модуль сам через %s", err, dm)
	}
	defer cl2.Close()
	done := fmt.Sprintf(`set -e
%[1]s
mkdir -p %[5]s
echo "installed=$(date +%%Y-%%m-%%dT%%H:%%M:%%S%%z) run=%[2]s code=%[4]s" > %[5]s/%[3]s
echo "  • новое подключение есть, мёртвая рука снята"
`, pf.disarm(run, unit), run, module, CodeHash(module), pf.marks)
	out, err = sh(cl2, done)
	emit(out)
	if err != nil {
		return fmt.Errorf("не удалось снять мёртвую руку (%v): узел откатит модуль через %s", err, dm)
	}
	return nil
}
