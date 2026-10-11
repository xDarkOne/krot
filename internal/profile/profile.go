// Package profile — сверка версий компонентов: эталон (пины этого пульта), узлы и свежие релизы
// у авторов. Отвечает на два вопроса: что на узлах уже новее эталона и что нового вышло, чтобы
// сначала попробовать на тестовых серверах.
package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/modexec"
	"github.com/xDarkOne/vpnkit/internal/remote"
)

// Component — компонент с закреплённой версией.
type Component struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`  // в эталоне (пин пульта)
	Repo string `json:"repo"` // owner/repo на GitHub
	Tags bool   `json:"-"`    // версии — теги, а не релизы
}

// Components — что закреплено в эталоне.
func Components() []Component {
	return []Component{
		{Name: "amneziawg", Ref: strings.TrimPrefix(modexec.AWGTag, "v"), Repo: "amnezia-vpn/amneziawg-linux-kernel-module", Tags: true},
		{Name: "awg-tools", Ref: strings.TrimPrefix(modexec.AWGToolsTag, "v"), Repo: "amnezia-vpn/amneziawg-tools", Tags: true},
		{Name: "xray", Ref: modexec.XrayVersion, Repo: "XTLS/Xray-core"},
		{Name: "sing-box", Ref: modexec.SingBoxVersion, Repo: "SagerNet/sing-box"},
		{Name: "telemt", Ref: modexec.TelemtVersion, Repo: "telemt/telemt"},
	}
}

// Версии на узле: только чтение.
const script = `echo "amneziawg=$(modinfo -F version amneziawg 2>/dev/null)"
echo "awg-tools=$(awg --version 2>/dev/null | head -1 | awk '{print $2}' | sed 's/^v//')"
echo "xray=$(/usr/local/bin/xray version 2>/dev/null | awk 'NR==1{print $2}')"
echo "sing-box=$(sing-box version 2>/dev/null | awk 'NR==1{print $3}')"
t=$(/usr/local/bin/telemt --version 2>/dev/null | awk '{print $2}')
[ -n "$t" ] || t=$(docker exec mtproxyl telemt --version 2>/dev/null | awk '{print $2}')
echo "telemt=$t"
`

// NodeVersions — версии компонентов на узле (пусто — компонента нет).
func NodeVersions(ctx context.Context, n *inventory.Node) (map[string]string, error) {
	cl, err := remote.Dial(n)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	r, err := cl.Run(ctx, script)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, l := range strings.Split(r.Stdout, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			out[k] = v
		}
	}
	return out, nil
}

// Latest — свежая версия у авторов (GitHub; запрос с этого компьютера). Пре-релизы не берутся.
func Latest(ctx context.Context, c Component) (string, error) {
	api := "https://api.github.com/repos/" + c.Repo + "/releases/latest"
	if c.Tags {
		api = "https://api.github.com/repos/" + c.Repo + "/tags?per_page=1"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", api, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", fmt.Errorf("GitHub ответил %d", r.StatusCode)
	}
	var tag string
	if c.Tags {
		var ts []struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&ts); err != nil || len(ts) == 0 {
			return "", fmt.Errorf("тегов нет")
		}
		tag = ts[0].Name
	} else {
		var rel struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rel); err != nil {
			return "", err
		}
		tag = rel.TagName
	}
	return strings.TrimPrefix(tag, "v"), nil
}

// Row — строка сверки по компоненту.
type Row struct {
	Component
	Nodes  map[string]string `json:"nodes"` // узел → версия
	Latest string            `json:"latest"`
	Note   string            `json:"note"`
}

// Diff сверяет эталон с узлами и свежими релизами.
func Diff(ctx context.Context, nodes []*inventory.Node) ([]Row, map[string]error) {
	errs := map[string]error{}
	vers := map[string]map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		go func(n *inventory.Node) {
			defer wg.Done()
			v, err := NodeVersions(ctx, n)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[n.Name] = err
				return
			}
			vers[n.Name] = v
		}(n)
	}
	var rows []Row
	for _, c := range Components() {
		rows = append(rows, Row{Component: c, Nodes: map[string]string{}})
	}
	for i := range rows { // по указателю на элемент среза: копия строки потеряла бы ответ
		wg.Add(1)
		go func(r *Row) { defer wg.Done(); r.Latest, _ = Latest(ctx, r.Component) }(&rows[i])
	}
	wg.Wait()
	for i := range rows {
		r := &rows[i]
		ahead := []string{}
		for _, n := range nodes {
			v := vers[n.Name][r.Name]
			r.Nodes[n.Name] = v
			if v != "" && v != r.Ref {
				ahead = append(ahead, n.Name+" "+v)
			}
		}
		switch {
		case len(ahead) > 0:
			r.Note = "на узлах другая версия (" + strings.Join(ahead, ", ") + "): если она проверена — поднять пин эталона"
		case r.Latest != "" && r.Latest != r.Ref:
			r.Note = "вышла " + r.Latest + ": сначала проверить на тестовых серверах, затем поднять пин"
		case r.Latest == "":
			r.Note = "свежую версию узнать не удалось"
		default:
			r.Note = "эталон = свежий релиз"
		}
	}
	return rows, errs
}
