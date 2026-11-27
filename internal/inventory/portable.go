package inventory

// PortableOptionalModules are the additional modules selected by the portable cascade preset.
var PortableOptionalModules = []string{
	"client-awg-legacy", "client-wireguard", "aivpn", "csqtt", "vk-turn-proxy",
	"exit-socks", "vpn-bot", "vpn-panel",
}

// ApplyPortableCascade adds fresh-install defaults. An explicitly configured choice wins.
func (inv *Inventory) ApplyPortableCascade() {
	useExitSocks := true
	for _, n := range inv.Nodes {
		if n.Role == "exit" && n.Modules["exit-socks"]["enabled"] == false {
			useExitSocks = false
		}
	}
	for _, n := range inv.Nodes {
		switch n.Role {
		case "entry":
			n.defaults("client-awg", map[string]any{"port": 51822})
			n.defaults("client-awg-legacy", map[string]any{"enabled": true, "port": 51820})
			n.defaults("client-wireguard", map[string]any{"enabled": true, "port": 51821})
			n.defaults("hysteria2", map[string]any{"per_client": true})
			if useExitSocks {
				n.defaults("xray", map[string]any{"socks_exit_domains": []any{
					"gemini.google.com", "aistudio.google.com", "generativelanguage.googleapis.com",
					"notebooklm.google.com", "gemini.gstatic.com",
				}})
			}
			for _, m := range []string{"aivpn", "csqtt", "vk-turn-proxy", "vpn-bot", "vpn-panel"} {
				n.defaults(m, map[string]any{"enabled": true})
			}
		case "exit":
			n.defaults("exit-socks", map[string]any{"enabled": true})
		}
	}
}

func (n *Node) defaults(module string, values map[string]any) {
	if n.Modules == nil {
		n.Modules = map[string]map[string]any{}
	}
	if n.Modules[module] == nil {
		n.Modules[module] = map[string]any{}
	}
	for key, value := range values {
		if _, exists := n.Modules[module][key]; !exists {
			n.Modules[module][key] = value
		}
	}
}
