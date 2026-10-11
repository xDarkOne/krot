package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xDarkOne/krot/internal/alerts"
	"github.com/xDarkOne/krot/internal/bootstrap"
	"github.com/xDarkOne/krot/internal/cloudflare"
	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/profile"
	"github.com/xDarkOne/krot/internal/reach"
	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/internal/secrets"
	"github.com/xDarkOne/krot/internal/status"
	"github.com/xDarkOne/krot/internal/wizard"
	"github.com/xDarkOne/krot/modules"
)

// Мастер первой установки. Пароли root живут только в аргументах вызовов и нигде не
// сохраняются: после перевода на ключ они больше не нужны.

var nodeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)

// WServer — сервер, как его ввели в мастере.
type WServer struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	Role         string `json:"role"`         // entry | exit | solo
	RootPassword string `json:"rootPassword"` // только для проверки и перевода на ключ
	Via          string `json:"via"`          // IP входа, если выход отсюда не виден (идём через вход по ключу)
	ViaName      string `json:"viaName"`      // имя узла входа (для пути к его ключу)
}

func keyDir() string { return filepath.Join(remote.StateDir(), "keys") }

func (s WServer) node() (*inventory.Node, error) {
	if !nodeName.MatchString(s.Name) {
		return nil, fmt.Errorf("имя узла %q: латиница в нижнем регистре, цифры и дефис, до 16 знаков", s.Name)
	}
	if strings.Count(s.Host, ".") != 3 {
		return nil, fmt.Errorf("%q — не IPv4-адрес", s.Host)
	}
	n := &inventory.Node{Name: s.Name, Host: s.Host, Role: s.Role,
		SSH: inventory.SSH{User: "root", Port: 22, Auth: "key", Key: filepath.Join(keyDir(), s.Name+"_ed25519")}}
	if s.Via != "" {
		n.SSH.Jump = &inventory.Jump{Host: s.Via, Port: 22, User: "root", Key: filepath.Join(keyDir(), s.ViaName+"_ed25519")}
	}
	return n, nil
}

// WReach — доступен ли сервер с этого компьютера (для входа — обязательно, P-57).
func (a *App) WReach(host string) reach.Verdict {
	return reach.SSH(a.ctx, host, 22, nil)
}

// WCheck — проверка сервера по паролю root под роль; ничего не меняет.
func (a *App) WCheck(s WServer) (wizard.ServerCheck, error) {
	n, err := s.node()
	if err != nil {
		return wizard.ServerCheck{}, err
	}
	return wizard.CheckServer(a.ctx, n, s.RootPassword), nil
}

// WBootstrap — перевод сервера на ключ пульта (пароль запрещается только после проверки ключа).
func (a *App) WBootstrap(s WServer) error {
	n, err := s.node()
	if err != nil {
		return err
	}
	if s.RootPassword == "" {
		return errors.New("нужен пароль root")
	}
	log := func(l string) { runtime.EventsEmit(a.ctx, "wizard-log", l) }
	return bootstrap.Run(a.ctx, n, s.RootPassword, log)
}

// WDNS — смотрит ли имя на адрес у публичных резолверов.
func (a *App) WDNS(name, ip string) reach.DNSResult { return reach.DNS(a.ctx, name, ip) }

// WSetup — что мастер запишет в inventory.
type WSetup struct {
	Topology   string          `json:"topology"` // cascade (вход + выход) | solo (один сервер)
	Entry      WServer         `json:"entry"`    // в solo — единственный сервер
	Exit       WServer         `json:"exit"`
	Domain     string          `json:"domain"`     // пусто — без домена
	TunnelName string          `json:"tunnelName"` // имя туннелей на входе (es.<домен>)
	SubName    string          `json:"subName"`    // подписки и маска Telegram (ru.<домен>)
	ExitName   string          `json:"exitName"`   // витрина выхода (en.<домен>), только каскад
	Choices    map[string]bool `json:"choices"`    // модуль → включён (только то, что пользователь менял)
	Dir        string          `json:"dir"`        // куда положить inventory.yaml и secrets.age
}

// build — inventory из ответов мастера (без записи на диск: его же показывает предпросмотр).
func (w WSetup) build() (*inventory.Inventory, error) {
	var en, xn []string
	if w.Domain != "" {
		en = uniq(w.TunnelName, w.SubName)
		xn = uniq(w.ExitName)
	}
	var inv *inventory.Inventory
	switch w.Topology {
	case "solo":
		if !nodeName.MatchString(w.Entry.Name) {
			return nil, fmt.Errorf("имя узла %q: латиница в нижнем регистре, цифры и дефис, до 16 знаков", w.Entry.Name)
		}
		inv = inventory.NewSolo(w.Entry.Name, w.Entry.Host, keyDir(), en)
	case "cascade", "":
		if w.Entry.Name == w.Exit.Name {
			return nil, errors.New("у входа и выхода должны быть разные имена")
		}
		inv = inventory.NewCascade(w.Entry.Name, w.Entry.Host, w.Exit.Name, w.Exit.Host, keyDir(), en, xn)
		if w.Exit.Via != "" { // выход отсюда не виден — ходим через вход
			e := inv.Nodes[w.Entry.Name]
			inv.Nodes[w.Exit.Name].SSH.Jump = &inventory.Jump{Host: w.Entry.Host, Port: 22, User: "root", Key: e.SSH.Key}
		}
	default:
		return nil, fmt.Errorf("схема %q: допустимы cascade и solo", w.Topology)
	}
	inv.Domain = w.Domain
	e := inv.Nodes[w.Entry.Name]
	if w.Domain != "" {
		e.Modules["nginx-tls"]["tunnel_name"] = w.TunnelName
		e.Modules["subscriptions"] = map[string]any{"sub_name": w.SubName}
		e.Modules["mtproxy"] = map[string]any{"mask_name": w.SubName}
	}
	// выбор пользователя пишется, только где он отличается от умолчания каталога —
	// иначе inventory зашумлён, а смена умолчаний в новых версиях до него не дойдёт
	topo := inv.Topo()
	for _, m := range modules.Offered() {
		on, ok := w.Choices[m.Name]
		c := m.Catalog
		if !ok || c.Required || c.Auto || on == c.Default {
			continue
		}
		for _, role := range c.Place[topo] {
			for _, n := range inv.Nodes {
				if n.Role != role {
					continue
				}
				if n.Modules[m.Name] == nil {
					n.Modules[m.Name] = map[string]any{}
				}
				n.Modules[m.Name]["enabled"] = on
			}
		}
	}
	return inv, nil
}

// WCreate пишет inventory и хранилище (на ключе пульта) и открывает их в приложении.
func (a *App) WCreate(w WSetup) (State, error) {
	inv, err := w.build()
	if err != nil {
		return State{}, err
	}
	dir := w.Dir
	if dir == "" {
		base := "cascade"
		if inv.Topo() == "solo" {
			base = "solo"
		}
		dir = freeDir(filepath.Join(remote.StateDir(), base))
	}
	path := filepath.Join(dir, "inventory.yaml")
	if _, err := os.Stat(path); err == nil {
		return State{}, fmt.Errorf("%s уже есть — выберите другую папку", path)
	}
	if err := inv.Save(path); err != nil {
		return State{}, err
	}
	// хранилище секретов на ключе пульта: без пароля, ключ — ~/.krot/age.key
	id, err := secrets.LoadKey()
	if err != nil {
		return State{}, err
	}
	if id == nil {
		if id, err = secrets.CreateKey(); err != nil {
			return State{}, err
		}
	}
	sp := secrets.DefaultPath(path)
	if !secrets.Exists(sp) {
		s, err := secrets.CreateWithKey(sp, id)
		if err != nil {
			return State{}, err
		}
		if err := s.Save(); err != nil {
			return State{}, err
		}
	}
	if err := a.open(path); err != nil {
		return State{}, err
	}
	return a.GetState(), nil
}

// WCatalogView — каталог для шага «Состав»: разделы и модули схемы по порядку.
type WCatalogView struct {
	Groups  []modules.Group `json:"groups"`
	Modules []ModuleView    `json:"modules"`
}

// WCatalog — что можно поставить в схеме topo (без узлов: только каталог).
func (a *App) WCatalog(topo string) (WCatalogView, error) {
	if _, ok := modules.Topologies[topo]; !ok {
		return WCatalogView{}, fmt.Errorf("схема %q: допустимы cascade и solo", topo)
	}
	out := WCatalogView{Groups: modules.Groups}
	for _, m := range modules.Offered() {
		if len(m.Catalog.Place[topo]) > 0 {
			out.Modules = append(out.Modules, ModuleView{Name: m.Name, Catalog: m.Catalog})
		}
	}
	return out, nil
}

// WPreviewView — план по ответам мастера до записи inventory и что в нём мешает.
type WPreviewView struct {
	Steps    []wizard.Step `json:"steps"`
	Problems []string      `json:"problems"`
}

// WPreview — что будет поставлено при таком выборе (ничего не пишет и никуда не ходит).
func (a *App) WPreview(w WSetup) (WPreviewView, error) {
	inv, err := w.build()
	if err != nil {
		return WPreviewView{}, err
	}
	plan, err := wizard.Plan(inv, nil)
	if err != nil {
		return WPreviewView{}, err
	}
	return WPreviewView{Steps: plan, Problems: wizard.Problems(plan)}, nil
}

func uniq(xs ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func freeDir(base string) string {
	d := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(d, "inventory.yaml")); err != nil {
			return d
		}
		d = fmt.Sprintf("%s-%d", base, i)
	}
}

// WPlan — план каскада для открытого inventory: что уже стоит, что поставить.
func (a *App) WPlan() ([]wizard.Step, error) {
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	if inv == nil {
		return nil, errors.New("не открыт файл узлов")
	}
	installed := map[string][]string{}
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		st := status.Get(a.ctx, n)
		if !st.Reachable || st.Error != "" {
			return nil, fmt.Errorf("%s: нет связи — %s", n.Name, st.Error)
		}
		for _, m := range st.Modules {
			installed[n.Name] = append(installed[n.Name], m.Name)
		}
	}
	return wizard.Plan(inv, installed)
}

// WDeploy ставит каскад по плану; ход — событиями "deploy-log". Повтор продолжит с места сбоя.
func (a *App) WDeploy() error {
	plan, err := a.WPlan()
	if err != nil {
		return err
	}
	if pr := wizard.Problems(plan); len(pr) > 0 {
		return errors.New(strings.Join(pr, "; "))
	}
	sec, err := a.vault()
	if err != nil {
		return err
	}
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	return wizard.Run(a.ctx, inv, sec, plan, func(e wizard.Event) { runtime.EventsEmit(a.ctx, "deploy-log", e) })
}

// KeyPath — где лежит ключ пульта (последний шаг мастера: сохраните копию).
func (a *App) KeyPath() string { return secrets.KeyPath() }

// --- оповещения в Telegram ---------------------------------------------------

// AlertsView — состояние настроек оповещений (токен наружу не отдаётся).
type AlertsView struct {
	Configured bool   `json:"configured"`
	Chats      string `json:"chats"`
	Via        string `json:"via"` // с какого узла идут запросы к Telegram
}

// alertNode — узел, с которого ходить в Telegram API: выход (из России API часто закрыт).
func (a *App) alertNode() (*inventory.Node, error) {
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	if inv == nil {
		return nil, errors.New("не открыт файл узлов")
	}
	var pick *inventory.Node
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		if n.Role == "exit" || pick == nil {
			pick = n
		}
	}
	if pick == nil {
		return nil, errors.New("нет серверов для запросов к Telegram")
	}
	return pick, nil
}

// AlertsState — настроены ли оповещения.
func (a *App) AlertsState() (AlertsView, error) {
	n, err := a.alertNode()
	if err != nil {
		return AlertsView{}, err
	}
	sec, err := a.vault()
	if err != nil {
		return AlertsView{}, err
	}
	t, _ := sec.Get(alerts.TokenKey)
	c, _ := sec.Get(alerts.ChatsKey)
	return AlertsView{Configured: t != "" && c != "", Chats: c, Via: n.Name}, nil
}

// AlertsBot — проверка токена: имя бота и кто ему писал (чтобы найти свой chat id).
type AlertsBot struct {
	Bot   string        `json:"bot"`
	Chats []alerts.Chat `json:"chats"`
}

// AlertsCheck — токен годится? Пустой токен — взять сохранённый.
func (a *App) AlertsCheck(token string) (AlertsBot, error) {
	n, err := a.alertNode()
	if err != nil {
		return AlertsBot{}, err
	}
	if token == "" {
		sec, err := a.vault()
		if err != nil {
			return AlertsBot{}, err
		}
		token, _ = sec.Get(alerts.TokenKey)
	}
	name, err := alerts.BotName(a.ctx, n, token)
	if err != nil {
		return AlertsBot{}, err
	}
	list, err := alerts.RecentChats(a.ctx, n, token)
	if err != nil {
		return AlertsBot{}, err
	}
	return AlertsBot{Bot: name, Chats: list}, nil
}

// AlertsSave — тестовое сообщение получателям и, если дошло, сохранение в хранилище.
func (a *App) AlertsSave(token, chats string) error {
	n, err := a.alertNode()
	if err != nil {
		return err
	}
	sec, err := a.vault()
	if err != nil {
		return err
	}
	if token == "" {
		token, _ = sec.Get(alerts.TokenKey)
	}
	ids, err := alerts.ParseChats(chats)
	if err != nil {
		return err
	}
	if err := alerts.Send(a.ctx, n, token, ids, "Крот: оповещения подключены ✓"); err != nil {
		return fmt.Errorf("тестовое сообщение не дошло: %w", err)
	}
	if err := sec.Set(alerts.TokenKey, token); err != nil {
		return err
	}
	if err := sec.Set(alerts.ChatsKey, strings.Join(ids, " ")); err != nil {
		return err
	}
	return sec.Save()
}

// AlertsApply — переставить monitoring на узлах, где он стоит: узлы получат токен.
// Ход — событиями "install-log".
func (a *App) AlertsApply() ([]string, error) {
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	if inv == nil {
		return nil, errors.New("не открыт файл узлов")
	}
	var done []string
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		st := status.Get(a.ctx, n)
		has := false
		for _, m := range st.Modules {
			has = has || m.Name == "monitoring"
		}
		if !has {
			continue
		}
		if err := a.Install(n.Name, "monitoring"); err != nil {
			return done, fmt.Errorf("%s: %w", n.Name, err)
		}
		done = append(done, n.Name)
	}
	return done, nil
}

// WCloudflare создаёт недостающие A-записи через API Cloudflare (токен не сохраняется).
// want: имя → IPv4. Существующие записи с другим адресом не трогаются — конфликт.
func (a *App) WCloudflare(token, domain string, want map[string]string) ([]cloudflare.Record, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("нужен токен API Cloudflare")
	}
	return cloudflare.Ensure(a.ctx, token, domain, want)
}

// --- обновления --------------------------------------------------------------

func (a *App) installedCodes() (*inventory.Inventory, map[string]map[string]string, error) {
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	if inv == nil {
		return nil, nil, errors.New("не открыт файл узлов")
	}
	out := map[string]map[string]string{}
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		st := status.Get(a.ctx, n)
		if !st.Reachable || st.Error != "" {
			return nil, nil, fmt.Errorf("%s: нет связи — %s", n.Name, st.Error)
		}
		out[n.Name] = map[string]string{}
		for _, m := range st.Modules {
			out[n.Name][m.Name] = m.Code
		}
	}
	return inv, out, nil
}

// Updates — какие модули на узлах отстали от этого пульта.
func (a *App) Updates() ([]wizard.Step, error) {
	inv, codes, err := a.installedCodes()
	if err != nil {
		return nil, err
	}
	if inv.Topology != "" && inv.Topology != "cascade" {
		return nil, nil
	}
	return wizard.Updates(inv, codes)
}

// ApplyUpdates — обновить отставшие модули по порядку каскада; ход — событиями "deploy-log".
func (a *App) ApplyUpdates() error {
	inv, codes, err := a.installedCodes()
	if err != nil {
		return err
	}
	plan, err := wizard.Updates(inv, codes)
	if err != nil {
		return err
	}
	sec, err := a.vault()
	if err != nil {
		return err
	}
	return wizard.Run(a.ctx, inv, sec, plan, func(e wizard.Event) { runtime.EventsEmit(a.ctx, "deploy-log", e) })
}

// VersionsView — сверка версий для окна.
type VersionsView struct {
	Nodes  []string          `json:"nodes"`
	Rows   []profile.Row     `json:"rows"`
	Errors map[string]string `json:"errors"`
}

// Versions — эталон (пины), узлы и свежие релизы у авторов.
func (a *App) Versions() (VersionsView, error) {
	a.mu.Lock()
	inv := a.inv
	a.mu.Unlock()
	if inv == nil {
		return VersionsView{}, errors.New("не открыт файл узлов")
	}
	var ns []*inventory.Node
	var names []string
	for _, name := range inv.NodeNames() {
		n, _ := inv.Node(name)
		ns = append(ns, n)
		names = append(names, n.Name)
	}
	rows, errs := profile.Diff(a.ctx, ns)
	out := VersionsView{Nodes: names, Rows: rows, Errors: map[string]string{}}
	for k, e := range errs {
		out.Errors[k] = e.Error()
	}
	return out, nil
}
