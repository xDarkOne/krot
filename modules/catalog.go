package modules

import (
	"sort"
	"sync"
)

// Схемы установки. Роли узлов в схеме — в порядке установки (выход раньше входа).
const (
	Cascade = "cascade" // вход в России → выход за границей
	Solo    = "solo"    // один зарубежный сервер
)

// Topologies — схема → роли её узлов.
var Topologies = map[string][]string{
	Cascade: {"exit", "entry"},
	Solo:    {"solo"},
}

// Catalog — как модуль предлагается при установке: место в каждой схеме, выбор по
// умолчанию и пояснения для человека. Пишется в манифесте, блок catalog.
type Catalog struct {
	Group string `yaml:"group" json:"group"` // base | cascade | access | extra — см. Groups
	Order int    `yaml:"order" json:"order"` // порядок показа и установки: от основы к надстройкам
	// Required — ставится всегда, выключить нельзя. Auto — не выбирается сам по себе:
	// ставится, когда его требует выбранный модуль. Default — включён, пока пользователь не выключил.
	Required bool `yaml:"required" json:"required"`
	Auto     bool `yaml:"auto" json:"auto"`
	Default  bool `yaml:"default" json:"default"`
	// Domain — нужны имена (nginx-tls.names): без домена модуль не ставится.
	Domain bool `yaml:"domain" json:"domain"`
	// Place — схема → роли узлов, на которые модуль ставится. Нет схемы — модуля в ней нет.
	Place map[string][]string `yaml:"place" json:"place"`
	Title string              `yaml:"title" json:"title"`
	What  string              `yaml:"what" json:"what"` // одна строка: что это
	Why   string              `yaml:"why" json:"why"`   // зачем и когда включать
	Cost  string              `yaml:"cost" json:"cost"` // чего стоит: память, порты, время
}

// Group — раздел каталога в окне.
type Group struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note"`
}

// Groups — разделы по порядку показа.
var Groups = []Group{
	{"base", "Основа", "Защита сервера, сертификаты, копии и мониторинг"},
	{"cascade", "Каскад", "Связка двух серверов — ставится сама в схеме с каскадом"},
	{"access", "Способы подключения", "Как устройства подключаются к VPN — нужен хотя бы один"},
	{"extra", "Дополнительно", "По необходимости"},
}

// Placed — ставится ли модуль на узел с этой ролью в этой схеме.
func (m Module) Placed(topo, role string) bool {
	if m.Catalog == nil {
		return false
	}
	for _, r := range m.Catalog.Place[topo] {
		if r == role {
			return true
		}
	}
	return false
}

// Needs — какие модули нужны этому на узле с ролью role в схеме topo
// (requires плюс requires_if по роли или схеме).
func (m Module) Needs(role, topo string) []string {
	out := append([]string{}, m.Requires...)
	for k, v := range m.RequiresIf {
		if k == role || k == topo {
			out = append(out, v...)
		}
	}
	return out
}

var (
	cacheOnce sync.Once
	cache     map[string]Module
	cacheErr  error
)

// All — манифесты (встроенные, читаются один раз).
func All() (map[string]Module, error) {
	cacheOnce.Do(func() { cache, cacheErr = Load() })
	return cache, cacheErr
}

// Offered — модули каталога по порядку установки.
func Offered() []Module {
	all, _ := All()
	var out []Module
	for _, m := range all {
		if m.Catalog != nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Catalog.Order < out[j].Catalog.Order })
	return out
}

// TitleOf — название модуля для человека; у модуля вне каталога — имя как есть.
func TitleOf(name string) string {
	all, _ := All()
	if m, ok := all[name]; ok && m.Catalog != nil && m.Catalog.Title != "" {
		return m.Catalog.Title
	}
	return name
}
