package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/xDarkOne/vpnkit/internal/clients"
	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/modexec"
	"github.com/xDarkOne/vpnkit/internal/remote"
	"github.com/xDarkOne/vpnkit/internal/secrets"
	"github.com/xDarkOne/vpnkit/internal/status"
	"github.com/xDarkOne/vpnkit/internal/wizard"
	"github.com/xDarkOne/vpnkit/modules"
)

// App — мост между окном и пакетами пульта. Вся логика та же, что у CLI vpnkit:
// приложение только показывает и спрашивает.
type App struct {
	ctx     context.Context
	mu      sync.Mutex
	invPath string
	inv     *inventory.Inventory
	sec     *secrets.Store
	pass    string // пароль хранилища, если оно не на ключе пульта (только в памяти)

	askMu  sync.Mutex          // один вопрос за раз: параллельные проверки узлов ждут
	asks   map[int]chan answer // открытые вопросы окну
	askSeq int
	known  map[string]string // ответы на вопросы о паролях (ключ) — только в памяти
	busy   map[string]string // узел → модуль, который на нём сейчас ставится
}

type answer struct {
	value string
	ok    bool
}

// ask показывает окно с вопросом и ждёт ответа (до 5 минут).
func (a *App) ask(question string) (string, error) {
	a.askMu.Lock()
	defer a.askMu.Unlock()
	a.mu.Lock()
	a.askSeq++
	id := a.askSeq
	ch := make(chan answer, 1)
	a.asks[id] = ch
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.asks, id); a.mu.Unlock() }()
	runtime.EventsEmit(a.ctx, "ask-password", map[string]any{"id": id, "question": question})
	select {
	case r := <-ch:
		if !r.ok {
			return "", errors.New("ввод пароля отменён")
		}
		return r.value, nil
	case <-time.After(5 * time.Minute):
		return "", errors.New("пароль не введён за 5 минут")
	}
}

// Answer — ответ окна на вопрос о пароле.
func (a *App) Answer(id int, value string, ok bool) {
	a.mu.Lock()
	ch := a.asks[id]
	a.mu.Unlock()
	if ch != nil {
		ch <- answer{value, ok}
	}
}

// ErrNeedPass — хранилище зашифровано паролем, а он ещё не введён.
var ErrNeedPass = errors.New("нужен пароль хранилища секретов")

func NewApp() *App { return &App{} }

func configPath() string { return filepath.Join(remote.StateDir(), "app.json") }

type appConfig struct {
	Inventory string `json:"inventory"`
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.asks = map[int]chan answer{}
	a.known = map[string]string{}
	a.busy = map[string]string{}
	// пароль хранилища и пароли ключей — окном; помним до закрытия приложения
	secrets.PassPrompt = func(q string) (string, error) {
		if a.pass != "" {
			return a.pass, nil
		}
		p, err := a.ask(q)
		if err == nil {
			a.pass = p
		}
		return p, err
	}
	remote.Prompt = func(q string) (string, error) {
		a.mu.Lock()
		p, ok := a.known[q]
		a.mu.Unlock()
		if ok {
			return p, nil
		}
		p, err := a.ask(q)
		if err == nil {
			a.mu.Lock()
			a.known[q] = p
			a.mu.Unlock()
		}
		return p, err
	}
	var c appConfig
	if b, err := os.ReadFile(configPath()); err == nil && json.Unmarshal(b, &c) == nil && c.Inventory != "" {
		_ = a.open(c.Inventory) // не открылось — экран предложит выбрать файл
	}
}

// NodeView — узел для списка слева.
type NodeView struct {
	Name string `json:"name"`
	Role string `json:"role"`
	Host string `json:"host"`
}

// State — что сейчас открыто.
type State struct {
	Inventory string     `json:"inventory"`
	Nodes     []NodeView `json:"nodes"`
	Vault     string     `json:"vault"` // key | pass | locked | none
	Error     string     `json:"error"`
	// Open — «узел/вид/имя»: открыть карточку клиента при запуске (VPNKIT_APP_OPEN;
	// для проверки интерфейса без щелчков мышью)
	Open string `json:"open"`
}

func (a *App) open(path string) error {
	inv, err := inventory.Load(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.invPath, a.inv, a.sec = path, inv, nil
	a.mu.Unlock()
	b, _ := json.Marshal(appConfig{Inventory: path})
	_ = os.MkdirAll(remote.StateDir(), 0o700)
	return os.WriteFile(configPath(), b, 0o600)
}

// vault открывает хранилище секретов при первой надобности.
func (a *App) vault() (*secrets.Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sec != nil {
		return a.sec, nil
	}
	if a.inv == nil {
		return nil, errors.New("не открыт файл узлов (inventory)")
	}
	s, err := secrets.Open(secrets.DefaultPath(a.invPath))
	if err != nil {
		return nil, err
	}
	a.sec = s
	return s, nil
}

// GetState — текущий файл узлов и список узлов.
func (a *App) GetState() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := State{Inventory: a.invPath, Vault: "none", Open: os.Getenv("VPNKIT_APP_OPEN"), Nodes: []NodeView{}}
	if a.inv == nil {
		return st
	}
	for _, name := range a.inv.NodeNames() {
		n, _ := a.inv.Node(name)
		st.Nodes = append(st.Nodes, NodeView{Name: n.Name, Role: n.Role, Host: n.Host})
	}
	sort.Slice(st.Nodes, func(i, j int) bool {
		ri, rj := roleOrder[st.Nodes[i].Role], roleOrder[st.Nodes[j].Role]
		if ri != rj {
			return ri < rj
		}
		return st.Nodes[i].Name < st.Nodes[j].Name
	})
	switch {
	case a.sec != nil:
		st.Vault = "open"
	case secrets.Exists(secrets.DefaultPath(a.invPath)):
		st.Vault = "locked"
	}
	return st
}

var roleOrder = map[string]int{"entry": 0, "solo": 1, "exit": 2}

// PickInventory — выбрать файл узлов в диалоге.
func (a *App) PickInventory() (State, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Файл узлов vpnkit (inventory.yaml)",
		Filters: []runtime.FileFilter{{DisplayName: "inventory (*.yaml)", Pattern: "*.yaml;*.yml"}},
	})
	if err != nil || p == "" {
		return a.GetState(), err
	}
	if err := a.open(p); err != nil {
		return a.GetState(), err
	}
	return a.GetState(), nil
}

// Unlock — пароль хранилища (если оно не на ключе пульта).
func (a *App) Unlock(pass string) error {
	a.pass = pass
	if _, err := a.vault(); err != nil {
		a.pass = ""
		return err
	}
	return nil
}

func (a *App) node(name string) (*inventory.Node, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inv == nil {
		return nil, errors.New("не открыт файл узлов")
	}
	return a.inv.Node(name)
}

// Status — сводка по узлу (SSH, только чтение).
func (a *App) Status(name string) (status.Status, error) {
	n, err := a.node(name)
	if err != nil {
		return status.Status{}, err
	}
	return status.Get(a.ctx, n), nil
}

// ClientList — клиенты узла по видам. Ошибка вида не мешает остальным (модуля может не быть).
type ClientList struct {
	AWG    []string          `json:"awg"`
	Sub    []string          `json:"sub"`
	TG     []string          `json:"tg"`
	Errors map[string]string `json:"errors"`
}

// Clients — все клиенты узла.
func (a *App) Clients(name string) (ClientList, error) {
	out := ClientList{Errors: map[string]string{}}
	n, err := a.node(name)
	if err != nil {
		return out, err
	}
	if n.Role == "exit" {
		return out, nil // клиенты живут на входе
	}
	if sec, err := a.vault(); err != nil {
		out.Errors["awg"] = err.Error()
	} else {
		for _, x := range clients.Names(n, sec) {
			out.AWG = append(out.AWG, strings.SplitN(x, " ", 2)[0])
		}
	}
	if l, err := clients.SubNames(a.ctx, n); err != nil {
		out.Errors["sub"] = err.Error()
	} else {
		out.Sub = l
	}
	if _, l, err := clients.TGClient(a.ctx, n, "list", ""); err != nil {
		out.Errors["tg"] = err.Error()
	} else {
		out.TG = l
	}
	return out, nil
}

// CardView — карточка клиента для экрана: QR картинкой.
type CardView struct {
	clients.Card
	QRImage string `json:"qrImage"` // data:image/png;base64,…
}

func (a *App) card(node, kind, name string, add bool) (CardView, error) {
	n, err := a.node(node)
	if err != nil {
		return CardView{}, err
	}
	var c clients.Card
	switch kind {
	case "awg":
		sec, err := a.vault()
		if err != nil {
			return CardView{}, err
		}
		var p clients.Profile
		if add {
			p, err = clients.Add(a.ctx, n, "", name, sec, func(s string) { runtime.EventsEmit(a.ctx, "log", s) })
		} else {
			p, err = clients.Show(n, "", name, sec)
		}
		if err != nil {
			return CardView{}, err
		}
		c = clients.AWGCard(n.Name, p)
	case "sub":
		var s clients.SubCard
		if add {
			s, err = clients.SubAdd(a.ctx, n, name)
		} else {
			s, err = clients.SubShow(a.ctx, n, name)
		}
		if err != nil {
			return CardView{}, err
		}
		c = clients.SubCardOf(n.Name, s)
	case "tg":
		op := "show"
		if add {
			op = "add"
		}
		t, _, err := clients.TGClient(a.ctx, n, op, name)
		if err != nil {
			return CardView{}, err
		}
		c = clients.TGCardOf(n.Name, t)
	default:
		return CardView{}, fmt.Errorf("неизвестный вид клиента %q", kind)
	}
	if err := c.Save(); err != nil {
		return CardView{}, fmt.Errorf("файлы на компьютере не сохранены: %w", err)
	}
	png, err := c.QRPNG(512)
	if err != nil {
		return CardView{}, err
	}
	return CardView{Card: c, QRImage: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, nil
}

// AddClient — новый клиент: ключи на узле, карточка с QR.
func (a *App) AddClient(node, kind, name string) (CardView, error) {
	return a.card(node, kind, name, true)
}

// ShowClient — карточка существующего клиента.
func (a *App) ShowClient(node, kind, name string) (CardView, error) {
	return a.card(node, kind, name, false)
}

// DelClient — удалить клиента: доступ пропадает сразу, у остальных ничего не рвётся.
func (a *App) DelClient(node, kind, name string) error {
	n, err := a.node(node)
	if err != nil {
		return err
	}
	switch kind {
	case "awg":
		sec, err := a.vault()
		if err != nil {
			return err
		}
		err = clients.Del(a.ctx, n, "", name, sec, func(s string) { runtime.EventsEmit(a.ctx, "log", s) })
		if err != nil {
			return err
		}
	case "sub":
		if err := clients.SubDel(a.ctx, n, name); err != nil {
			return err
		}
	case "tg":
		if _, _, err := clients.TGClient(a.ctx, n, "del", name); err != nil {
			return err
		}
	default:
		return fmt.Errorf("неизвестный вид клиента %q", kind)
	}
	clients.RemoveFiles(n.Name, kind, name)
	return nil
}

// OpenClientsFolder — открыть папку с файлами клиентов узла.
func (a *App) OpenClientsFolder(node string) error {
	dir := clients.Dir(node)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	cmd := "xdg-open"
	switch goruntime.GOOS {
	case "windows":
		cmd = "explorer"
	case "darwin":
		cmd = "open"
	}
	return exec.Command(cmd, dir).Start()
}

// ModuleView — модуль для списка на экране узла.
type ModuleView struct {
	Name      string           `json:"name"`
	Catalog   *modules.Catalog `json:"catalog"`
	Choice    wizard.Choice    `json:"choice"` // выбран ли в inventory и почему
	Installed bool             `json:"installed"`
	Missing   []string         `json:"missing"` // какие нужные модули ещё не стоят (названия для людей)
	Busy      bool             `json:"busy"`
}

// Modules — что установщик умеет ставить на этот узел, по порядку от основы к надстройкам.
// installed — модули, которые уже стоят (из последнего Status).
func (a *App) Modules(node string, installed []string) ([]ModuleView, error) {
	n, err := a.node(node)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, m := range installed {
		have[m] = true
	}
	a.mu.Lock()
	topo, busy := a.inv.Topo(), a.busy[n.Name]
	a.mu.Unlock()
	sel := wizard.Select(topo, n)
	var out []ModuleView
	for _, m := range modules.Offered() { // уже по порядку от основы к надстройкам
		if !m.Placed(topo, n.Role) {
			continue
		}
		v := ModuleView{Name: m.Name, Catalog: m.Catalog, Choice: sel[m.Name], Installed: have[m.Name], Busy: busy == m.Name}
		for _, need := range m.Needs(n.Role, topo) {
			if !have[need] {
				v.Missing = append(v.Missing, modules.TitleOf(need))
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// Install ставит модуль тем же путём, что `vpnkit install`: снимок, «мёртвая рука», проверка,
// откат при ошибке. Ход работы — событиями "install-log" {node, module, line}.
func (a *App) Install(node, module string) error {
	n, err := a.node(node)
	if err != nil {
		return err
	}
	a.mu.Lock()
	if m := a.busy[n.Name]; m != "" {
		a.mu.Unlock()
		return fmt.Errorf("на %s уже ставится %s — дождитесь окончания", n.Name, m)
	}
	a.busy[n.Name] = module
	inv := a.inv
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.busy, n.Name); a.mu.Unlock() }()
	sec, err := a.vault()
	if err != nil {
		return err
	}
	emit := func(s string) {
		runtime.EventsEmit(a.ctx, "install-log", map[string]string{"node": n.Name, "module": module, "line": s})
	}
	emit("== " + n.Name + ": " + module)
	if err := modexec.Install(a.ctx, inv, n, module, sec, emit); err != nil {
		emit("  ✗ " + err.Error())
		return err
	}
	emit("  ✓ " + module + " установлен")
	return nil
}
