package modexec

import (
	"testing"

	"github.com/xDarkOne/krot/internal/inventory"
)

func TestPrepareTrafficGuardExemptions(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{name: "omitted"},
		{name: "empty list", cfg: map[string]any{"exempt": []any{}}},
		{
			name: "YAML list",
			cfg:  map[string]any{"exempt": []any{"198.51.100.4", "2001:db8::/64"}},
			want: "198.51.100.4 2001:db8::/64",
		},
		{
			name: "space-separated string",
			cfg:  map[string]any{"exempt": "198.51.100.4  2001:db8::/64"},
			want: "198.51.100.4 2001:db8::/64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &inventory.Node{
				Name: "ru", Role: "entry", Host: "192.0.2.1",
				Modules: map[string]map[string]any{"traffic-guard": tt.cfg},
			}
			inv := &inventory.Inventory{Nodes: map[string]*inventory.Node{"ru": node}}
			p, err := Prepare(inv, node, "traffic-guard", nil)
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := p["EXEMPT"]; !ok || got != tt.want {
				t.Errorf("EXEMPT = %q (set: %v), want %q", got, ok, tt.want)
			}
			if p["NODES"] != node.Host {
				t.Errorf("NODES = %q, want %q", p["NODES"], node.Host)
			}
		})
	}
}
