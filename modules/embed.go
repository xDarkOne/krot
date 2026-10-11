// Package modules встраивает манифесты модулей (*.yaml) в бинарник krot
// и даёт к ним типизированный доступ.
package modules

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed *.yaml
var fs embed.FS

// Param — вопрос, который установщик задаёт пользователю.
type Param struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Default any    `yaml:"default"`
	Ask     string `yaml:"ask"`
	Check   string `yaml:"check"`
}

// Port — порт, который модуль занимает.
type Port struct {
	Proto   string `yaml:"proto"`
	Port    any    `yaml:"port"`
	Expose  bool   `yaml:"expose"`
	Purpose string `yaml:"purpose"`
}

// Health — одна проверка здоровья. Node пуст — проверка выполняется на узле модуля.
type Health struct {
	Node   string `yaml:"node"`
	Cmd    string `yaml:"cmd"`
	Expect string `yaml:"expect"`
}

// Module — манифест модуля (схема: _schema.yaml).
type Module struct {
	Name        string              `yaml:"name"`
	Doc         string              `yaml:"doc"`
	Roles       []string            `yaml:"roles"`
	Topology    []string            `yaml:"topology"`
	Requires    []string            `yaml:"requires"`
	RequiresIf  map[string][]string `yaml:"requires_if"`
	Conflicts   []string            `yaml:"conflicts"`
	Resources   map[string]any      `yaml:"resources"`
	Params      []Param             `yaml:"params"`
	Secrets     []string            `yaml:"secrets"`
	Ports       []Port              `yaml:"ports"`
	Units       []string            `yaml:"units"`
	Files       []string            `yaml:"files"`
	Versions    map[string]any      `yaml:"versions"`
	Constraints []string            `yaml:"constraints"`
	Health      []Health            `yaml:"health"`
	Pitfalls    []string            `yaml:"pitfalls"`
	Steps       map[string]string   `yaml:"steps"`
	// DeadmanSec — через сколько узел откатит модуль сам, если пульт не подтвердил
	// успех. Короткий для сетевых модулей (риск отрезать SSH), длинный для сборок.
	DeadmanSec int `yaml:"deadman_sec"`
	// Catalog — как модуль предлагается при установке; nil — установщик его не предлагает
	// (описан, но ставится вручную).
	Catalog *Catalog `yaml:"catalog"`
}

// HasRole сообщает, допустим ли модуль на роли.
func (m Module) HasRole(role string) bool {
	for _, r := range m.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Resource возвращает ресурс строкой (значения бывают числом или текстом).
func (m Module) Resource(key string) string {
	v, ok := m.Resources[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(v)
}

// Load читает все встроенные манифесты.
func Load() (map[string]Module, error) {
	entries, err := fs.ReadDir(".")
	if err != nil {
		return nil, err
	}
	out := map[string]Module{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".yaml") || strings.HasPrefix(n, "_") {
			continue
		}
		b, err := fs.ReadFile(n)
		if err != nil {
			return nil, err
		}
		var m Module
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		out[m.Name] = m
	}
	return out, nil
}

// Names — имена модулей по алфавиту.
func Names(all map[string]Module) []string {
	var n []string
	for k := range all {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}
