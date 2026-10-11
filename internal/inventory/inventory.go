// Package inventory загружает inventory.yaml — описание узлов, ролей и модулей.
// Секретов здесь нет: пароли берутся из переменных окружения или спрашиваются.
package inventory

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SSH — как подключаться к узлу.
type SSH struct {
	User        string `yaml:"user"`
	Port        int    `yaml:"port"`
	Auth        string `yaml:"auth"` // key | password
	Key         string `yaml:"key"`
	PasswordEnv string `yaml:"password_env"`
	// Jump — промежуточный узел (как ssh -J): для серверов, недоступных напрямую,
	// например заблокированных РКН. Вход на него — только по ключу.
	Jump *Jump `yaml:"jump"`
}

// Jump — промежуточный SSH-узел.
type Jump struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	User string `yaml:"user"`
	Key  string `yaml:"key"`
}

// Node — один узел.
type Node struct {
	Name    string                    `yaml:"-"`
	Host    string                    `yaml:"host"`
	SSH     SSH                       `yaml:"ssh"`
	Role    string                    `yaml:"role"` // entry | exit | solo
	Profile string                    `yaml:"profile"`
	Modules map[string]map[string]any `yaml:"modules"`
	Vars    map[string]string         `yaml:"vars"`
}

// Inventory — весь файл.
type Inventory struct {
	Version  int               `yaml:"version"`
	Domain   string            `yaml:"domain"`
	Topology string            `yaml:"topology"`
	Vars     map[string]string `yaml:"vars"`
	Nodes    map[string]*Node  `yaml:"nodes"`
}

var validRoles = map[string]bool{"entry": true, "exit": true, "solo": true}

// Load читает и проверяет inventory.
func Load(path string) (*Inventory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv Inventory
	if err := yaml.Unmarshal(b, &inv); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(inv.Nodes) == 0 {
		return nil, fmt.Errorf("%s: нет ни одного узла (nodes)", path)
	}
	if inv.Topology != "" && inv.Topology != "cascade" && inv.Topology != "solo" {
		return nil, fmt.Errorf("%s: topology %q — допустимы cascade (два сервера) и solo (один)", path, inv.Topology)
	}
	for name, n := range inv.Nodes {
		n.Name = name
		if n.Host == "" {
			return nil, fmt.Errorf("узел %s: не задан host", name)
		}
		if !validRoles[n.Role] {
			return nil, fmt.Errorf("узел %s: роль %q, допустимы entry|exit|solo", name, n.Role)
		}
		if n.SSH.User == "" {
			n.SSH.User = "root"
		}
		if n.SSH.Port == 0 {
			n.SSH.Port = 22
		}
		if j := n.SSH.Jump; j != nil {
			if j.Host == "" {
				return nil, fmt.Errorf("узел %s: ssh.jump без host", name)
			}
			if j.User == "" {
				j.User = "root"
			}
			if j.Port == 0 {
				j.Port = 22
			}
		}
		if n.SSH.Auth == "" {
			n.SSH.Auth = "key"
		}
	}
	return &inv, nil
}

// Topo — схема: cascade (вход и выход) или solo (один сервер); пусто — cascade.
func (inv *Inventory) Topo() string {
	if inv.Topology == "" {
		return "cascade"
	}
	return inv.Topology
}

// Node возвращает узел по имени.
func (inv *Inventory) Node(name string) (*Node, error) {
	n, ok := inv.Nodes[name]
	if !ok {
		return nil, fmt.Errorf("узла %q нет в inventory (есть: %s)", name, strings.Join(inv.NodeNames(), ", "))
	}
	return n, nil
}

// NodeNames — имена узлов по алфавиту.
func (inv *Inventory) NodeNames() []string {
	var out []string
	for k := range inv.Nodes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// VarsFor — переменные для подстановки в проверки: общие, затем узла.
// Добавляет host каждого узла как <имя>_ip и <роль>_ip.
func (inv *Inventory) VarsFor(n *Node) map[string]string {
	v := map[string]string{"domain": inv.Domain}
	for _, other := range inv.Nodes {
		v[other.Name+"_ip"] = other.Host
		v[other.Role+"_ip"] = other.Host
	}
	for k, val := range inv.Vars {
		v[k] = val
	}
	for k, val := range n.Vars {
		v[k] = val
	}
	return v
}
