package inventory

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// newNode — узел из ответов мастера. Ключ — keyDir/<имя>_ed25519 (его создаст bootstrap).
// names — имена nginx-tls (пусто — без домена: модули, которым нужен домен, не ставятся).
func newNode(name, host, role, keyDir string, names []string) *Node {
	n := &Node{Name: name, Host: host, Role: role,
		SSH:     SSH{User: "root", Port: 22, Auth: "key", Key: filepath.Join(keyDir, name+"_ed25519")},
		Modules: map[string]map[string]any{}}
	if len(names) > 0 {
		var l []any
		for _, x := range names {
			l = append(l, x)
		}
		n.Modules["nginx-tls"] = map[string]any{"names": l}
	}
	return n
}

// NewCascade — inventory каскада из ответов мастера: вход в России, выход за границей.
func NewCascade(entryName, entryHost, exitName, exitHost, keyDir string, entryNames, exitNames []string) *Inventory {
	return &Inventory{Version: 1, Topology: "cascade", Nodes: map[string]*Node{
		entryName: newNode(entryName, entryHost, "entry", keyDir, entryNames),
		exitName:  newNode(exitName, exitHost, "exit", keyDir, exitNames),
	}}
}

// NewSolo — inventory одного зарубежного сервера: клиенты подключаются к нему и выходят с него.
func NewSolo(name, host, keyDir string, names []string) *Inventory {
	return &Inventory{Version: 1, Topology: "solo", Nodes: map[string]*Node{
		name: newNode(name, host, "solo", keyDir, names),
	}}
}

// Save пишет inventory атомарно (права 600: адреса и пути к ключам — не секрет, но и не для всех).
func (inv *Inventory) Save(path string) error {
	var b bytes.Buffer
	b.WriteString("# krot: узлы, роли и модули. Секретов здесь нет — они в secrets.age рядом.\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(inv); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b.Bytes(), 0o600); err != nil {
		return err
	}
	if _, err := Load(tmp); err != nil { // то, что записали, должно читаться
		os.Remove(tmp)
		return fmt.Errorf("записанный inventory не читается: %w", err)
	}
	return os.Rename(tmp, path)
}
