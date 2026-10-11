// Package clients — клиенты входов AmneziaWG: добавить пира на живой вход, собрать профиль,
// ссылку vpn:// (AmneziaVPN) и QR. Профили хранятся в secrets.age: <узел>/clients/<имя>/<вход>.
package clients

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/xDarkOne/vpnkit/internal/awgkeys"
	"github.com/xDarkOne/vpnkit/internal/inventory"
	"github.com/xDarkOne/vpnkit/internal/remote"
	"github.com/xDarkOne/vpnkit/internal/secrets"
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Input — параметры клиентского входа, собранные из inventory и хранилища.
type Input struct {
	Iface, Net, GW, Endpoint, MTU string
	Port                          string
	ServerPublic, HPK             string
	Obfs                          awgkeys.Obfs
}

// LoadInput собирает параметры входа (вход должен быть установлен модулем client-awg).
func LoadInput(n *inventory.Node, iface string, sec *secrets.Store) (Input, error) {
	cfg := n.Modules["client-awg"]
	get := func(k, def string) string {
		if v, ok := cfg[k]; ok {
			return fmt.Sprint(v)
		}
		return def
	}
	if iface == "" {
		iface = get("iface", "awg2")
	}
	ns := n.Name + "/client-awg/" + iface + "/"
	priv, ok1 := sec.Get(ns + "private")
	port, ok2 := sec.Get(ns + "port")
	hpk, ok3 := sec.Get(ns + "hpk")
	ob, ok4 := sec.Get(ns + "obfs")
	if !(ok1 && ok2 && ok3 && ok4) {
		return Input{}, fmt.Errorf("в хранилище нет параметров входа %s на %s — сначала vpnkit install %s client-awg", iface, n.Name, n.Name)
	}
	pub, err := awgkeys.PublicOf(priv)
	if err != nil {
		return Input{}, err
	}
	var o awgkeys.Obfs
	if err := json.Unmarshal([]byte(ob), &o); err != nil {
		return Input{}, err
	}
	netw := get("net", "10.9.2.0/24")
	_, ipnet, err := net.ParseCIDR(netw)
	if err != nil {
		return Input{}, err
	}
	b := ipnet.IP.To4()
	return Input{
		Iface: iface, Net: netw, GW: net.IPv4(b[0], b[1], b[2], b[3]+1).String(),
		Endpoint: get("endpoint", n.Host), MTU: get("mtu", "1260"), Port: port,
		ServerPublic: pub, HPK: hpk, Obfs: o,
	}, nil
}

// Peer — клиент в конфиге входа.
type Peer struct{ Name, IP string }

// ReadPeers читает клиентов из конфига входа на узле (комментарий «# name:» + AllowedIPs).
func ReadPeers(ctx context.Context, cl *remote.Client, iface string) ([]Peer, error) {
	r, err := cl.Run(ctx, "cat /etc/amnezia/amneziawg/"+iface+".conf\n")
	if err != nil {
		return nil, err
	}
	if r.Code != 0 {
		return nil, fmt.Errorf("нет конфига входа %s на узле", iface)
	}
	var out []Peer
	name := ""
	for _, l := range strings.Split(r.Stdout, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "# name:") {
			name = strings.TrimSpace(strings.TrimPrefix(l, "# name:"))
		}
		// пиры без «# name:» (добавлены руками) тоже учитываются: иначе их адрес выдали бы повторно
		if strings.HasPrefix(l, "AllowedIPs") {
			v := strings.TrimSpace(strings.SplitN(l, "=", 2)[1])
			out = append(out, Peer{name, strings.TrimSuffix(strings.Split(v, ",")[0], "/32")})
			name = ""
		}
	}
	return out, nil
}

func freeIP(in Input, used []Peer) (string, error) {
	_, ipnet, _ := net.ParseCIDR(in.Net)
	b := ipnet.IP.To4()
	taken := map[string]bool{in.GW: true}
	for _, p := range used {
		taken[p.IP] = true
	}
	ones, bits := ipnet.Mask.Size()
	size := 1 << (bits - ones)
	for i := 2; i < size-1; i++ {
		ip := net.IPv4(b[0], b[1], b[2], b[3]+byte(i)).String()
		if !taken[ip] {
			return ip, nil
		}
	}
	return "", fmt.Errorf("в сети %s нет свободных адресов", in.Net)
}

// Profile — собранный профиль клиента.
type Profile struct {
	Name, IP, Conf, Link string
}

// BuildConf — профиль клиента для AmneziaVPN/awg-quick.
func BuildConf(in Input, ip, clientPriv, psk string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nAddress = %s/32\nDNS = %s\nMTU = %s\nPrivateKey = %s\n", ip, in.GW, in.MTU, clientPriv)
	b.WriteString(in.Obfs.Lines())
	fmt.Fprintf(&b, "HeaderProtectionKey = %s\nContentPaddingAddition = 0-16\n\n", in.HPK)
	fmt.Fprintf(&b, "[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = %s\nPersistentKeepalive = 25\n",
		in.ServerPublic, psk, net.JoinHostPort(in.Endpoint, in.Port))
	return b.String()
}

// VPNLink — ссылка vpn:// для AmneziaVPN (формат AmneziaVPN: JSON → zlib → 4 байта длины → base64url).
func VPNLink(name string, in Input, ip, conf, clientPriv, psk string) (string, error) {
	obf := map[string]string{}
	for _, l := range strings.Split(in.Obfs.Lines(), "\n") {
		if k, v, ok := strings.Cut(l, " = "); ok {
			obf[k] = v
		}
	}
	obf["HeaderProtectionKey"] = in.HPK
	obf["ContentPaddingAddition"] = "0-16"
	clientPub, err := awgkeys.PublicOf(clientPriv)
	if err != nil {
		return "", err
	}
	inner := map[string]any{}
	for k, v := range obf {
		inner[k] = v
	}
	port := 0
	fmt.Sscan(in.Port, &port)
	for k, v := range map[string]any{
		"allowed_ips": []string{"0.0.0.0/0", "::/0"}, "clientId": clientPub, "client_ip": ip,
		"client_priv_key": clientPriv, "client_pub_key": "", "config": conf, "hostName": in.Endpoint,
		"mtu": in.MTU, "persistent_keep_alive": "25", "port": port, "psk_key": psk, "server_pub_key": in.ServerPublic,
	} {
		inner[k] = v
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return "", err
	}
	awg := map[string]any{}
	for k, v := range obf {
		awg[k] = v
	}
	_, ipnet, _ := net.ParseCIDR(in.Net)
	for k, v := range map[string]any{
		"last_config": string(innerJSON), "port": in.Port, "transport_proto": "udp",
		"subnet_address": ipnet.IP.String(), "protocol_version": "3.1",
	} {
		awg[k] = v
	}
	obj := map[string]any{
		"containers":       []any{map[string]any{"awg": awg, "container": "amnezia-awg2"}},
		"defaultContainer": "amnezia-awg2", "description": name,
		"dns1": in.GW, "dns2": in.GW, "hostName": in.Endpoint,
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, 8)
	w.Write(raw)
	w.Close()
	buf := make([]byte, 4, 4+z.Len())
	binary.BigEndian.PutUint32(buf, uint32(len(raw)))
	buf = append(buf, z.Bytes()...)
	return "vpn://" + strings.TrimRight(base64.URLEncoding.EncodeToString(buf), "="), nil
}

// addPeer: копия конфига → дописать блок → живой awg set (без перезапуска входа) → проверка.
func addPeer(ctx context.Context, cl *remote.Client, iface, name, pub, psk, ip string) error {
	script := fmt.Sprintf(`set -e
umask 077
c=/etc/amnezia/amneziawg/%[1]s.conf
mkdir -p /var/lib/vpnkit/clients-backup
cp -a "$c" "/var/lib/vpnkit/clients-backup/%[1]s.conf.$(date +%%Y%%m%%d-%%H%%M%%S)"
ls -1t /var/lib/vpnkit/clients-backup/%[1]s.conf.* | tail -n +21 | xargs -r rm -f
printf '\n# name: %[2]s\n[Peer]\nPublicKey = %[3]s\nPresharedKey = %[4]s\nAllowedIPs = %[5]s/32\n' >> "$c"
f=$(mktemp); printf '%%s\n' '%[4]s' > "$f"
awg set %[1]s peer '%[3]s' preshared-key "$f" allowed-ips %[5]s/32
rm -f "$f"
awg show %[1]s allowed-ips | grep -q '%[5]s/32'
echo ok
`, iface, name, pub, psk, ip)
	r, err := cl.Run(ctx, script)
	if err != nil {
		return err
	}
	if r.Code != 0 || !strings.Contains(r.Stdout, "ok") {
		return fmt.Errorf("код %d: %s", r.Code, strings.TrimSpace(r.Stderr))
	}
	return nil
}

// Resync возвращает на вход пиры клиентов из хранилища пульта (сервер заменён или конфиг
// потерян): ключи и адреса клиентов те же, их профили продолжают работать. Пиры на узле,
// которых нет в хранилище, не трогаются — только перечисляются.
func Resync(ctx context.Context, n *inventory.Node, iface string, sec *secrets.Store, log func(string)) (added, extra []string, err error) {
	in, err := LoadInput(n, iface, sec)
	if err != nil {
		return nil, nil, err
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return nil, nil, err
	}
	defer cl.Close()
	peers, err := ReadPeers(ctx, cl, in.Iface)
	if err != nil {
		return nil, nil, err
	}
	on := map[string]bool{}
	for _, p := range peers {
		on[p.Name] = true
	}
	inVault := map[string]bool{}
	pfx := n.Name + "/clients/"
	for _, k := range sec.Keys() {
		if !strings.HasPrefix(k, pfx) || !strings.HasSuffix(k, "/"+in.Iface) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(k, pfx), "/"+in.Iface)
		inVault[name] = true
		if on[name] {
			continue
		}
		conf, _ := sec.Get(k)
		val := func(key string) string {
			for _, l := range strings.Split(conf, "\n") {
				if a, b, ok := strings.Cut(l, " = "); ok && a == key {
					return strings.TrimSpace(b)
				}
			}
			return ""
		}
		pub, err := awgkeys.PublicOf(val("PrivateKey"))
		if err != nil {
			return added, nil, fmt.Errorf("профиль %s в хранилище повреждён: %w", name, err)
		}
		ip := strings.TrimSuffix(val("Address"), "/32")
		if err := addPeer(ctx, cl, in.Iface, name, pub, val("PresharedKey"), ip); err != nil {
			return added, nil, fmt.Errorf("%s: %s", name, secrets.Mask(err.Error(), []string{val("PresharedKey")}))
		}
		log(fmt.Sprintf("пир %s (%s) возвращён на %s из хранилища", name, ip, in.Iface))
		added = append(added, name)
	}
	for _, p := range peers {
		switch {
		case p.Name == "":
			extra = append(extra, "без имени "+p.IP)
		case !inVault[p.Name]:
			extra = append(extra, p.Name)
		}
	}
	return added, extra, nil
}

// Add добавляет клиента на вход: пир в конфиг и в живой интерфейс (без перезапуска), профиль в хранилище.
func Add(ctx context.Context, n *inventory.Node, iface, name string, sec *secrets.Store, log func(string)) (Profile, error) {
	if !nameRe.MatchString(name) {
		return Profile{}, fmt.Errorf("имя клиента: латиница, цифры, _ и -, до 32 символов")
	}
	in, err := LoadInput(n, iface, sec)
	if err != nil {
		return Profile{}, err
	}
	key := n.Name + "/clients/" + name + "/" + in.Iface
	if _, ok := sec.Get(key); ok {
		return Profile{}, fmt.Errorf("клиент %s на %s уже есть (vpnkit clients show)", name, in.Iface)
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return Profile{}, err
	}
	defer cl.Close()
	peers, err := ReadPeers(ctx, cl, in.Iface)
	if err != nil {
		return Profile{}, err
	}
	for _, p := range peers {
		if p.Name == name {
			return Profile{}, fmt.Errorf("в конфиге %s уже есть клиент %s", in.Iface, name)
		}
	}
	ip, err := freeIP(in, peers)
	if err != nil {
		return Profile{}, err
	}
	kp, err := awgkeys.NewKeypair()
	if err != nil {
		return Profile{}, err
	}
	psk, err := awgkeys.NewSymmetric()
	if err != nil {
		return Profile{}, err
	}
	if err := addPeer(ctx, cl, in.Iface, name, kp.Public, psk, ip); err != nil {
		return Profile{}, fmt.Errorf("добавление пира на узле не удалось: %s", secrets.Mask(err.Error(), []string{psk, kp.Private}))
	}
	log(fmt.Sprintf("пир %s (%s) добавлен на %s без перезапуска входа", name, ip, in.Iface))

	conf := BuildConf(in, ip, kp.Private, psk)
	link, err := VPNLink(name, in, ip, conf, kp.Private, psk)
	if err != nil {
		return Profile{}, err
	}
	if err := sec.Set(key, conf); err != nil {
		return Profile{}, err
	}
	if err := sec.Save(); err != nil {
		return Profile{}, fmt.Errorf("пир добавлен, но профиль не сохранён в хранилище: %w", err)
	}
	return Profile{Name: name, IP: ip, Conf: conf, Link: link}, nil
}

// Del удаляет клиента со входа: копия конфига → блок пира из файла → awg set … remove
// (без перезапуска входа, остальные клиенты не замечают) → проверка → профиль из хранилища.
// Если пира на узле уже нет (удалён руками), чистится только хранилище.
func Del(ctx context.Context, n *inventory.Node, iface, name string, sec *secrets.Store, log func(string)) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("недопустимое имя %q", name)
	}
	in, err := LoadInput(n, iface, sec)
	if err != nil {
		return err
	}
	cl, err := remote.Dial(n)
	if err != nil {
		return err
	}
	defer cl.Close()
	script := fmt.Sprintf(`set -e
umask 077
c=/etc/amnezia/amneziawg/%[1]s.conf
mkdir -p /var/lib/vpnkit/clients-backup
python3 - "$c" '%[2]s' > /root/.vk-del.out <<'PY'
import sys
path, name = sys.argv[1], sys.argv[2]
lines = open(path).read().split("\n")
out, pub, i = [], "", 0
while i < len(lines):
    if lines[i].strip() == "# name: " + name:
        j = i + 1
        while j < len(lines) and not lines[j].startswith("# name:"):
            if lines[j].startswith("PublicKey = "):
                pub = lines[j].split(" = ", 1)[1].strip()
            j += 1
        while out and out[-1] == "":
            out.pop()
        out.append("")
        i = j
        continue
    out.append(lines[i])
    i += 1
print(pub)
if pub:
    open(path + ".vk-new", "w").write("\n".join(out).rstrip("\n") + "\n")
PY
pub=$(cat /root/.vk-del.out); rm -f /root/.vk-del.out
if [ -z "$pub" ]; then echo absent; exit 0; fi
cp -a "$c" "/var/lib/vpnkit/clients-backup/%[1]s.conf.$(date +%%Y%%m%%d-%%H%%M%%S)"
chmod 600 "$c.vk-new" && mv "$c.vk-new" "$c"
awg set %[1]s peer "$pub" remove
if awg show %[1]s peers | grep -qxF "$pub"; then echo "пир всё ещё в интерфейсе" >&2; exit 1; fi
echo removed
`, in.Iface, name)
	r, err := cl.Run(ctx, script)
	if err != nil || r.Code != 0 {
		return fmt.Errorf("удаление пира на узле не удалось: %v %s", err, strings.TrimSpace(r.Stderr))
	}
	key := n.Name + "/clients/" + name + "/" + in.Iface
	_, inVault := sec.Get(key)
	switch {
	case strings.Contains(r.Stdout, "removed"):
		log(fmt.Sprintf("пир %s снят с %s без перезапуска входа (копия конфига — в clients-backup)", name, in.Iface))
	case inVault:
		log(fmt.Sprintf("на %s пира %s уже нет — чищу только хранилище", in.Iface, name))
	default:
		return fmt.Errorf("клиента %s нет ни на %s, ни в хранилище", name, in.Iface)
	}
	if inVault {
		sec.Delete(key)
		if err := sec.Save(); err != nil {
			return fmt.Errorf("пир снят, но хранилище не сохранено: %w", err)
		}
	}
	return nil
}

// Show собирает профиль клиента из хранилища.
func Show(n *inventory.Node, iface, name string, sec *secrets.Store) (Profile, error) {
	in, err := LoadInput(n, iface, sec)
	if err != nil {
		return Profile{}, err
	}
	conf, ok := sec.Get(n.Name + "/clients/" + name + "/" + in.Iface)
	if !ok {
		return Profile{}, fmt.Errorf("в хранилище нет профиля %s на %s", name, in.Iface)
	}
	val := func(k string) string {
		for _, l := range strings.Split(conf, "\n") {
			if a, b, ok := strings.Cut(l, " = "); ok && a == k {
				return b
			}
		}
		return ""
	}
	ip := strings.TrimSuffix(val("Address"), "/32")
	link, err := VPNLink(name, in, ip, conf, val("PrivateKey"), val("PresharedKey"))
	return Profile{Name: name, IP: ip, Conf: conf, Link: link}, err
}

// Names — клиенты узла из хранилища.
func Names(n *inventory.Node, sec *secrets.Store) []string {
	pfx := n.Name + "/clients/"
	set := map[string]bool{}
	for _, k := range sec.Keys() {
		if strings.HasPrefix(k, pfx) {
			set[strings.SplitN(strings.TrimPrefix(k, pfx), "/", 2)[0]+" ("+k[strings.LastIndex(k, "/")+1:]+")"] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
