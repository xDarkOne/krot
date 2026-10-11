// Package health выполняет проверки здоровья из манифестов модулей (только чтение).
package health

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/internal/secrets"
	"github.com/xDarkOne/krot/modules"
)

// Status — итог проверки.
type Status int

const (
	Pass Status = iota
	Look        // вывод получен, оценить может только человек
	Skip        // не хватает переменной в inventory
	Bad
)

func (s Status) String() string {
	return [...]string{"ок", "смотреть", "пропуск", "ПЛОХО"}[s]
}

// Check — одна выполненная проверка.
type Check struct {
	Module string
	Cmd    string
	Expect string
	Output string
	Status Status
	Note   string
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// Substitute подставляет {переменные}. Конструкции вида %{…} (curl -w) не трогает.
// Возвращает имя первой недостающей переменной.
func Substitute(s string, vars map[string]string) (string, string) {
	var b strings.Builder
	missing := ""
	last := 0
	for _, m := range placeholder.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(s[last:m[0]])
		last = m[1]
		if m[0] > 0 && s[m[0]-1] == '%' {
			b.WriteString(s[m[0]:m[1]])
			continue
		}
		k := s[m[2]:m[3]]
		if v, ok := vars[k]; ok {
			b.WriteString(v)
			continue
		}
		if missing == "" {
			missing = k
		}
		b.WriteString(s[m[0]:m[1]])
	}
	b.WriteString(s[last:])
	return b.String(), missing
}

var cmpRe = regexp.MustCompile(`^(>=|<=|>|<)\s*(-?\d+)$`)
var numRe = regexp.MustCompile(`-?\d+`)

// Evaluate сопоставляет вывод с ожиданием. Понимает: rc=0, nonempty,
// сравнение числа (>N, >=N, <N, <=N), active, подстроку. Фразы для
// человека (с пробелами, которых нет в выводе) — Look.
func Evaluate(expect, out string, code int) (Status, string) {
	e := strings.TrimSpace(expect)
	o := strings.TrimSpace(out)
	if m := cmpRe.FindStringSubmatch(e); m != nil {
		want, _ := strconv.Atoi(m[2])
		s := numRe.FindString(o)
		if s == "" {
			return Bad, "в выводе нет числа"
		}
		got, _ := strconv.Atoi(s)
		ok := map[string]bool{">": got > want, ">=": got >= want, "<": got < want, "<=": got <= want}[m[1]]
		if ok {
			return Pass, ""
		}
		return Bad, fmt.Sprintf("%d, ожидалось %s%d", got, m[1], want)
	}
	switch {
	case e == "rc=0":
		if code == 0 {
			return Pass, ""
		}
		return Bad, fmt.Sprintf("код выхода %d", code)
	case e == "nonempty":
		if o != "" {
			return Pass, ""
		}
		return Bad, "пустой вывод"
	case e == "active":
		if o == "" {
			return Bad, "пустой вывод"
		}
		for _, w := range strings.Fields(o) {
			if w != "active" {
				return Bad, "не все службы active"
			}
		}
		return Pass, ""
	case e == "":
		return Look, ""
	case strings.Contains(o, e):
		return Pass, ""
	case strings.Contains(e, " ") && !strings.ContainsAny(e, "0123456789{"):
		return Look, "ожидание сформулировано для человека"
	}
	return Bad, "в выводе нет «" + e + "»"
}

// Run выполняет проверки всех модулей узла.
// Переменные для проверки модуля: общие и узла, затем настройки модуля
// из inventory, затем его секреты из хранилища. Вывод маскируется.
func Run(ctx context.Context, inv *inventory.Inventory, n *inventory.Node, cl *remote.Client, all map[string]modules.Module, sec *secrets.Store) []Check {
	base := inv.VarsFor(n)
	mask := sec.Values()
	// проверяются модули, которые на узле стоят (метки установщика); узел без меток —
	// по списку модулей в inventory
	var names []string
	if r, err := cl.Run(ctx, "ls /var/lib/vpnkit/modules 2>/dev/null\n"); err == nil {
		for _, l := range strings.Fields(r.Stdout) {
			if _, ok := all[l]; ok {
				names = append(names, l)
			}
		}
	}
	if len(names) == 0 {
		for k := range n.Modules {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	var out []Check
	for _, name := range names {
		m, ok := all[name]
		if !ok {
			out = append(out, Check{Module: name, Status: Skip, Note: "модуль неизвестен пульту"})
			continue
		}
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		for k, v := range n.Modules[name] {
			vars[k] = fmt.Sprint(v)
		}
		for k, v := range sec.ForModule(n.Name, name) {
			vars[k] = v
		}
		for _, h := range m.Health {
			if h.Node != "" && h.Node != n.Role {
				continue // проверка для другой роли (например, снаружи с exit)
			}
			cmd, miss := Substitute(h.Cmd, vars)
			exp, _ := Substitute(h.Expect, vars)
			c := Check{Module: name, Cmd: secrets.Mask(cmd, mask), Expect: exp}
			if miss != "" {
				c.Status, c.Note = Skip, fmt.Sprintf("нет {%s}: задайте в inventory (modules.%s.%s) или в хранилище (krot secrets set %s/%s/%s)", miss, name, miss, n.Name, name, miss)
				out = append(out, c)
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
			r, err := cl.Run(cctx, cmd+"\n")
			cancel()
			c.Output = secrets.Mask(strings.TrimSpace(r.Stdout+r.Stderr), mask)
			if err != nil {
				c.Status, c.Note = Bad, err.Error()
			} else {
				c.Status, c.Note = Evaluate(exp, c.Output, r.Code)
			}
			out = append(out, c)
		}
	}
	return out
}
