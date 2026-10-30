package wizard

import (
	"strings"
	"testing"

	"github.com/xDarkOne/krot/agent"
	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/modexec"
	"github.com/xDarkOne/krot/modules"
)

func node(name, role string, names ...string) *inventory.Node {
	n := &inventory.Node{Name: name, Role: role, Host: "1.1.1.1", Modules: map[string]map[string]any{}}
	if len(names) > 0 {
		var l []any
		for _, x := range names {
			l = append(l, x)
		}
		n.Modules["nginx-tls"] = map[string]any{"names": l}
	}
	return n
}

func inv(names bool) *inventory.Inventory {
	e, x := node("e", "entry"), node("x", "exit")
	if names {
		e, x = node("e", "entry", "ru.example.org"), node("x", "exit", "en.example.org")
	}
	return &inventory.Inventory{Topology: "cascade", Nodes: map[string]*inventory.Node{"e": e, "x": x}}
}

func solo(names bool) *inventory.Inventory {
	s := node("s", "solo")
	if names {
		s = node("s", "solo", "vpn.example.org")
	}
	return &inventory.Inventory{Topology: "solo", Nodes: map[string]*inventory.Node{"s": s}}
}

func todo(p []Step) []string {
	var out []string
	for _, s := range p {
		if !s.Done && !s.Skip {
			out = append(out, s.Node+":"+s.Module)
		}
	}
	return out
}

func reason(p []Step, node, module string) (bool, string) {
	for _, s := range p {
		if s.Node == node && s.Module == module {
			return !s.Skip, s.Reason
		}
	}
	return false, "нет в плане"
}

func TestPlanCascade(t *testing.T) {
	p, err := Plan(inv(true), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := todo(p)
	if len(got) != 23 || got[0] != "x:base-host" || got[1] != "e:base-host" || got[4] != "x:cascade-awg0" || got[5] != "e:cascade-awg0" {
		t.Fatalf("порядок: %v", got)
	}
	if on, why := reason(p, "e", "split-routing"); !on || !strings.Contains(why, "нужен для") {
		t.Errorf("split-routing на входе: %v %q", on, why)
	}
	p, _ = Plan(inv(false), nil)
	if n := len(todo(p)); n != 16 {
		t.Errorf("без домена шагов %d, ждали 16: %v", n, todo(p))
	}
	if on, why := reason(p, "e", "xray"); on || why != "нужен домен" {
		t.Errorf("xray без домена: %v %q", on, why)
	}
}

func TestPlanSolo(t *testing.T) {
	p, err := Plan(solo(true), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(todo(p), " ")
	want := "s:base-host s:amneziawg s:client-awg s:nginx-tls s:xray s:hysteria2 s:subscriptions s:mtproxy s:traffic-guard s:backup s:relocate s:monitoring"
	if got != want {
		t.Fatalf("solo:\n got %s\nwant %s", got, want)
	}
	// без AWG модуль ядра не нужен
	i := solo(true)
	i.Nodes["s"].Modules["client-awg"] = map[string]any{"enabled": false}
	p, _ = Plan(i, nil)
	if on, why := reason(p, "s", "amneziawg"); on || why != "не нужен" {
		t.Errorf("amneziawg без входа AWG: %v %q", on, why)
	}
	if pr := Problems(p); len(pr) != 0 {
		t.Errorf("подписка есть, а проблемы: %v", pr)
	}
	// без домена и без AWG подключаться нечем
	i = solo(false)
	i.Nodes["s"].Modules["client-awg"] = map[string]any{"enabled": false}
	p, _ = Plan(i, nil)
	if pr := Problems(p); len(pr) != 1 {
		t.Errorf("ждали одну проблему, получили %v", pr)
	}
	// в одиночной схеме нет ни каскада, ни выхода
	if _, err := Plan(&inventory.Inventory{Topology: "solo", Nodes: map[string]*inventory.Node{"e": node("e", "entry")}}, nil); err == nil {
		t.Error("узел entry в схеме solo должен быть ошибкой")
	}
}

func TestPlanChoices(t *testing.T) {
	i := inv(true)
	i.Nodes["e"].Modules["mtproxy"] = map[string]any{"enabled": false}
	i.Nodes["e"].Modules["nginx-tls"]["enabled"] = false
	p, _ := Plan(i, map[string][]string{"x": {"base-host", "amneziawg"}})
	for _, s := range p {
		if s.Node == "x" && (s.Module == "base-host" || s.Module == "amneziawg") && !s.Done {
			t.Errorf("%s:%s стоит, но не отмечен", s.Node, s.Module)
		}
	}
	if on, why := reason(p, "e", "mtproxy"); on || why != "выключен" {
		t.Errorf("mtproxy: %v %q", on, why)
	}
	// xray нужен сайт, а его выключили — xray не ставится
	if on, why := reason(p, "e", "xray"); on || !strings.Contains(why, "выключен") {
		t.Errorf("xray без nginx-tls: %v %q", on, why)
	}
	// основу выключить нельзя
	i.Nodes["e"].Modules["base-host"] = map[string]any{"enabled": false}
	p, _ = Plan(i, nil)
	if on, _ := reason(p, "e", "base-host"); !on {
		t.Error("base-host обязателен")
	}
}

func TestUpdates(t *testing.T) {
	i := inv(true)
	i.Nodes["x"].Modules["monitoring"] = map[string]any{"enabled": false}
	cur := modexec.CodeHash("base-host")
	up, err := Updates(i, map[string]map[string]string{
		"x": {"base-host": cur, "amneziawg": "old000000000", "monitoring": "old000000000"},
		"e": {"base-host": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range up {
		got = append(got, s.Node+":"+s.Module)
		if s.Skip {
			t.Errorf("%s:%s в обновлении помечен пропуском", s.Node, s.Module)
		}
	}
	// x:base-host актуален; x:amneziawg — старый; e:base-host — до учёта версий;
	// x:monitoring выключен пользователем, но стоит — обновляется; новых модулей нет
	if strings.Join(got, " ") != "e:base-host x:amneziawg x:monitoring" {
		t.Errorf("обновления: %v", got)
	}
}

// Каталог в манифестах: у каждого модуля есть скрипты, группа и пояснения; всё, что модулю
// нужно, стоит на том же узле раньше него (или вне каталога — тогда это забота пользователя).
func TestCatalog(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range modules.Groups {
		groups[g.ID] = true
	}
	all, err := modules.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range modules.Offered() {
		c := m.Catalog
		for _, step := range []string{"install", "rollback"} {
			if _, ok := agent.Step(m.Name, step); !ok {
				t.Errorf("%s в каталоге, но нет agent/%s/%s.sh", m.Name, m.Name, step)
			}
		}
		if !groups[c.Group] || c.Title == "" || c.What == "" || c.Why == "" {
			t.Errorf("%s: группа %q, название, «что» и «зачем» обязательны", m.Name, c.Group)
		}
		if len(c.Place) == 0 {
			t.Errorf("%s: не указано, куда ставится (place)", m.Name)
		}
		for topo, roles := range c.Place {
			for _, role := range roles {
				for _, d := range m.Needs(role, topo) {
					dm, ok := all[d]
					if !ok {
						t.Errorf("%s требует неизвестный модуль %s", m.Name, d)
						continue
					}
					if dm.Catalog == nil {
						continue
					}
					if !dm.Placed(topo, role) {
						t.Errorf("%s (%s/%s) требует %s, а тот туда не ставится", m.Name, topo, role, d)
					}
					if dm.Catalog.Order >= c.Order {
						t.Errorf("%s требует %s, но стоит в порядке раньше него", m.Name, d)
					}
				}
			}
		}
	}
}
