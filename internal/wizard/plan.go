// Package wizard — установка целиком: какие модули на каком узле (выбор пользователя плюс
// зависимости), в каком порядке, и прогон по шагам через modexec. Общий для мастера в окне
// и для `vpnkit deploy`. Что предлагается и куда ставится — блок catalog в манифестах.
package wizard

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/modexec"
	"github.com/xDarkOne/vpnkit/internal/secrets"
	"github.com/xDarkOne/vpnkit/modules"
)

// Step — один шаг плана.
type Step struct {
	Node   string `json:"node"`
	Role   string `json:"role"`
	Module string `json:"module"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`   // уже стоит — шаг пропускается
	Reason string `json:"reason"` // почему модуль ставится или нет: «по умолчанию», «нужен для …», «нужен домен»
	Skip   bool   `json:"skip"`
	From   string `json:"from,omitempty"` // версия кода на узле (для обновлений; пусто — до учёта версий)
	To     string `json:"to,omitempty"`   // версия кода в этом пульте
}

// Choice — ставится ли модуль на узел и почему.
type Choice struct {
	On  bool   `json:"on"`
	Why string `json:"why"`
}

// flag — выбор пользователя в inventory: modules.<имя>.enabled (set=false — не выбирал).
func flag(n *inventory.Node, module string) (on, set bool) {
	cfg, ok := n.Modules[module]
	if !ok {
		return false, false
	}
	v, ok := cfg["enabled"]
	if !ok {
		return false, false
	}
	return fmt.Sprint(v) != "false", true
}

// Disabled — модуль выключен пользователем в inventory: modules.<имя>.enabled: false.
func Disabled(n *inventory.Node, module string) bool {
	on, set := flag(n, module)
	return set && !on
}

func hasNames(n *inventory.Node) bool {
	v, ok := n.Modules["nginx-tls"]["names"]
	return ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != "[]"
}

// Select — что из каталога ставится на узел в схеме topo. Модуль включён, если он обязателен,
// выбран пользователем (enabled: true) или включён по умолчанию и не выключен; к включённым
// добавляется всё, что им нужно на этом узле. Модуль, которому не хватает домена или нужного
// модуля, выключенного пользователем, не ставится — Why говорит почему.
func Select(topo string, n *inventory.Node) map[string]Choice {
	placed := map[string]modules.Module{}
	for _, m := range modules.Offered() {
		if m.Placed(topo, n.Role) {
			placed[m.Name] = m
		}
	}
	// blocker — почему модуль нельзя поставить (с учётом того, что нужно ему); "" — можно
	memo := map[string]string{}
	var blocker func(name string) string
	blocker = func(name string) string {
		if r, ok := memo[name]; ok {
			return r
		}
		memo[name] = "" // защита от циклов: манифесты их не содержат, тест это проверяет
		m := placed[name]
		r := ""
		switch {
		case m.Catalog.Domain && !hasNames(n):
			r = "нужен домен"
		case Disabled(n, name) && !m.Catalog.Required:
			r = "выключен"
		}
		for _, d := range m.Needs(n.Role, topo) {
			if r != "" {
				break
			}
			if _, ok := placed[d]; ok {
				if dr := blocker(d); dr != "" {
					r = fmt.Sprintf("нужен «%s» — %s", modules.TitleOf(d), dr)
				}
			}
		}
		memo[name] = r
		return r
	}

	out := map[string]Choice{}
	var on []string
	for name, m := range placed {
		c := m.Catalog
		v, set := flag(n, name)
		why := ""
		switch {
		case c.Required:
			why = "обязателен"
		case c.Auto:
			out[name] = Choice{false, "не нужен"}
			continue
		case set && v:
			why = "выбран"
		case set:
			out[name] = Choice{false, "выключен"}
			continue
		case c.Default:
			why = "по умолчанию"
		default:
			out[name] = Choice{false, "не выбран"}
			continue
		}
		if r := blocker(name); r != "" {
			out[name] = Choice{false, r}
			continue
		}
		out[name] = Choice{true, why}
		on = append(on, name)
	}
	// зависимости включённых: blocker уже проверил, что они ставятся
	sort.Strings(on)
	for len(on) > 0 {
		name := on[0]
		on = on[1:]
		for _, d := range placed[name].Needs(n.Role, topo) {
			if _, ok := placed[d]; !ok || out[d].On {
				continue
			}
			out[d] = Choice{true, fmt.Sprintf("нужен для «%s»", modules.TitleOf(name))}
			on = append(on, d)
		}
	}
	return out
}

// Nodes — узлы схемы по ролям (по одному на роль).
func Nodes(inv *inventory.Inventory) (map[string]*inventory.Node, error) {
	topo := inv.Topo()
	roles, ok := modules.Topologies[topo]
	if !ok {
		return nil, fmt.Errorf("неизвестная схема %q", topo)
	}
	byRole := map[string]*inventory.Node{}
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		if byRole[n.Role] != nil {
			return nil, fmt.Errorf("в inventory больше одного узла с ролью %s", n.Role)
		}
		byRole[n.Role] = n
	}
	for _, r := range roles {
		if byRole[r] == nil {
			return nil, fmt.Errorf("в схеме %s нужен узел с ролью %s (узлы: %s)", topo, r, strings.Join(roles, ", "))
		}
	}
	for r, n := range byRole {
		found := false
		for _, x := range roles {
			found = found || x == r
		}
		if !found {
			return nil, fmt.Errorf("узел %s с ролью %s не входит в схему %s (роли: %s)", n.Name, r, topo, strings.Join(roles, ", "))
		}
	}
	return byRole, nil
}

// Plan — шаги для inventory: модули каталога по порядку, на каждом узле схемы (выход раньше
// входа). installed: узел → модули, что уже стоят (метки на узлах).
func Plan(inv *inventory.Inventory, installed map[string][]string) ([]Step, error) {
	byRole, err := Nodes(inv)
	if err != nil {
		return nil, err
	}
	topo := inv.Topo()
	sel := map[string]map[string]Choice{}
	for r, n := range byRole {
		sel[r] = Select(topo, n)
	}
	var out []Step
	for _, m := range modules.Offered() {
		for _, role := range m.Catalog.Place[topo] {
			n := byRole[role]
			c := sel[role][m.Name]
			s := Step{Node: n.Name, Role: role, Module: m.Name, Title: m.Catalog.Title, Skip: !c.On, Reason: c.Why}
			for _, x := range installed[n.Name] {
				if x == m.Name {
					s.Done = true
				}
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// Problems — чего не хватает плану, чтобы им можно было пользоваться: на узле, к которому
// подключаются клиенты, должен быть хотя бы один способ подключения.
func Problems(plan []Step) []string {
	var out []string
	access := map[string]bool{}
	clientNode := map[string]bool{}
	all, _ := modules.All()
	for _, s := range plan {
		if s.Role == "entry" || s.Role == "solo" {
			clientNode[s.Node] = true
		}
		if m, ok := all[s.Module]; ok && m.Catalog.Group == "access" && (!s.Skip || s.Done) {
			access[s.Node] = true
		}
	}
	for n := range clientNode {
		if !access[n] {
			out = append(out, fmt.Sprintf("на %s не выбран ни один способ подключения — клиентам нечем подключиться", n))
		}
	}
	sort.Strings(out)
	return out
}

// Event — ход прогона: номер шага, всего, строка журнала.
type Event struct {
	Step  int    `json:"step"`
	Total int    `json:"total"`
	Node  string `json:"node"`
	Title string `json:"title"`
	Line  string `json:"line"`
}

// Run ставит невыполненные шаги по порядку. На первой ошибке останавливается: шаг уже
// откатан modexec, а пройденные остаются — повторный Run продолжит с упавшего.
func Run(ctx context.Context, inv *inventory.Inventory, sec *secrets.Store, plan []Step, emit func(Event)) error {
	var todo []Step
	for _, s := range plan {
		if !s.Done && !s.Skip {
			todo = append(todo, s)
		}
	}
	for i, s := range todo {
		n, err := inv.Node(s.Node)
		if err != nil {
			return err
		}
		ev := Event{Step: i + 1, Total: len(todo), Node: s.Node, Title: s.Title}
		line := func(l string) { e := ev; e.Line = l; emit(e) }
		line(fmt.Sprintf("== шаг %d из %d: %s → %s", i+1, len(todo), s.Title, s.Node))
		if err := modexec.Install(ctx, inv, n, s.Module, sec, line); err != nil {
			line("  ✗ " + err.Error())
			return fmt.Errorf("шаг %d (%s на %s): %w", i+1, s.Title, s.Node, err)
		}
		line("  ✓ готово")
	}
	return nil
}

// Updates — план обновления: стоящие модули, чья версия кода на узле не совпадает с пультом
// (скрипты или пины поменялись). Новые модули обновление не ставит — это делает deploy.
// installed: узел → модуль → версия кода из метки (пусто — поставлен до учёта версий).
func Updates(inv *inventory.Inventory, installed map[string]map[string]string) ([]Step, error) {
	names := map[string][]string{}
	for n, ms := range installed {
		for m := range ms {
			names[n] = append(names[n], m)
		}
	}
	plan, err := Plan(inv, names)
	if err != nil {
		return nil, err
	}
	var out []Step
	for _, s := range plan {
		if !s.Done {
			continue
		}
		have, want := installed[s.Node][s.Module], modexec.CodeHash(s.Module)
		if have == want {
			continue
		}
		// стоящий модуль обновляется, даже если пользователь его с тех пор выключил:
		// снятие модуля — отдельное действие, а старый код на узле хуже нового
		s.Done, s.Skip, s.From, s.To = false, false, have, want
		out = append(out, s)
	}
	return out, nil
}
