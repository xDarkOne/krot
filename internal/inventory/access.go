package inventory

import (
	"fmt"
	"regexp"
)

// ClientAccess describes the default interface of an installable WireGuard-family entry.
type ClientAccess struct {
	Module, Iface, Net, Tool, MTU, MSS, RuleMain, RuleCascade string
}

var InterfaceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,14}$`)

var ClientAccesses = []ClientAccess{
	{"client-awg", "awg2", "10.9.2.0/24", "awg", "1260", "1220", "208", "212"},
	{"client-awg-legacy", "awg1", "10.9.0.0/24", "awg", "1260", "1220", "205", "210"},
	{"client-wireguard", "wg2", "10.9.1.0/24", "wg", "1420", "1380", "206", "211"},
}

func (a ClientAccess) ConfigDir() string {
	if a.Tool == "wg" {
		return "/etc/wireguard"
	}
	return "/etc/amnezia/amneziawg"
}

func (a ClientAccess) Value(n *Node, key, fallback string) string {
	if value, ok := n.Modules[a.Module][key]; ok {
		return fmt.Sprint(value)
	}
	return fallback
}

func (n *Node) AccessForModule(module string) ClientAccess {
	for _, a := range ClientAccesses {
		if a.Module == module {
			a.Iface = a.Value(n, "iface", a.Iface)
			return a
		}
	}
	return ClientAccess{}
}

func (n *Node) AccessForInterface(iface string) ClientAccess {
	primary := n.AccessForModule("client-awg")
	if iface == "" || iface == primary.Iface {
		return primary
	}
	for _, item := range ClientAccesses[1:] {
		a := n.AccessForModule(item.Module)
		if a.Iface == iface {
			return a
		}
	}
	primary.Iface = iface
	return primary
}
