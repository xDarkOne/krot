package modexec

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/remote"
	"github.com/xDarkOne/krot/internal/secrets"
)

const (
	aivpnMasks = "38ffc697aadf99893b5e8e1813d6c39110309bbff958d8bb32f48258071fd3e3"
	aivpnTag   = "v1.3.0"
	csqttTag   = "v2.5.0"
	csqttAPK   = "f99deeac7aae3960a49c20e71ed4ed05daea0997ad2dcc0aeadedd55d188f2e0"
)

type portableAsset struct {
	name   string
	digest string
}

var aivpnAssets = map[string]portableAsset{
	"amd64": {"aivpn-server-linux-x86_64", "13d38559f7c06802a3074f26d2c5ea4caa3ba803d44fe72d8860832521a24322"},
	"arm64": {"aivpn-server-linux-aarch64", "2b8fd3ea452f308b67aac99778e21974db3b29178c3f9b0e532547c911c1b0ee"},
}
var csqttAssets = map[string]portableAsset{
	"amd64": {"assets/csqtt-linux-amd64", "e6f5b785feda10991060f6ac024a8faf5e740b168a991ab2a0e065505b215e8c"},
	"arm64": {"assets/csqtt-linux-arm64", "d8930e19d628dec7715def67c6cc7a9a2d66120bbb7559b2db403763fa6fc73e"},
}

func transportPort(p map[string]string, name, fallback string) error {
	port := p[name]
	if port == "" {
		port = fallback
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s: нужен порт 1–65535", strings.ToLower(name))
	}
	p[name] = strconv.Itoa(number)
	return nil
}

func prepareExtraTransport(module string, n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	p["CASCADE"] = "1"
	if module == "aivpn" {
		ip := net.ParseIP(n.Host)
		if (ip == nil && !validRealityName(n.Host)) || (ip != nil && ip.To4() == nil) {
			return nil, fmt.Errorf("AIVPN: нужен IPv4 или публичное DNS-имя входа")
		}
		p["PUBLIC_HOST"] = n.Host
		p["IFACE"], p["NET"], p["GW"] = "aivpn0", "10.9.4.0/24", "10.9.4.1"
		return p, transportPort(p, "PORT", "8443")
	}
	p["IFACE"], p["NET"], p["GW"] = "csqtt1", "10.66.67.0/24", "10.66.67.1"
	if err := transportPort(p, "PORT", "46000"); err != nil {
		return nil, err
	}
	if err := transportPort(p, "WEB_PORT", "46002"); err != nil {
		return nil, err
	}
	if p["SSH_PORT"] == p["WEB_PORT"] {
		return nil, fmt.Errorf("WEB-порт CSQTT совпадает с SSH")
	}
	changed := false
	for _, item := range [][2]string{{"CSQTT_PASSWORD", "password"}, {"CSQTT_ADMIN_PASSWORD", "admin_password"}} {
		value, err := ensure(sec, n.Name+"/csqtt/"+item[1], func() (string, error) {
			var random [24]byte
			_, err := rand.Read(random[:])
			return hex.EncodeToString(random[:]), err
		}, &changed)
		if err != nil {
			return nil, err
		}
		p[item[0]] = value
	}
	if changed {
		return p, sec.Save()
	}
	return p, nil
}

func extraTransportArtifact(ctx context.Context, cl *remote.Client, module string) ([]Artifact, error) {
	assets, filename := aivpnAssets, "aivpn-server"
	if module == "csqtt" {
		assets, filename = csqttAssets, "csqtt"
	}
	r, err := cl.Run(ctx, "dpkg --print-architecture\nsha256sum /usr/local/bin/"+filename+" 2>/dev/null || true\n")
	if err != nil {
		return nil, err
	}
	lines := strings.Fields(r.Stdout)
	if r.Code != 0 || len(lines) == 0 {
		return nil, fmt.Errorf("%s: не удалось определить архитектуру узла", module)
	}
	asset, ok := assets[lines[0]]
	if !ok {
		return nil, fmt.Errorf("%s: архитектура %s не поддерживается", module, lines[0])
	}
	if module == "aivpn" {
		url := "https://github.com/infosave2007/aivpn/releases/download/" + aivpnTag + "/"
		masks, err := downloadArtifact(ctx, "aivpn-masks.tar.gz", url+"masks.tar.gz", aivpnMasks)
		if err != nil {
			return nil, err
		}
		if len(lines) > 1 && lines[1] == asset.digest {
			return []Artifact{masks}, nil
		}
		binary, err := downloadArtifact(ctx, filename, url+asset.name, asset.digest)
		if err != nil {
			return nil, err
		}
		return []Artifact{masks, binary}, nil
	}
	if len(lines) > 1 && lines[1] == asset.digest {
		return nil, nil
	}
	pkg, err := downloadArtifact(ctx, "CSQTT-universal.apk",
		"https://github.com/amurcanov/csqtt/releases/download/"+csqttTag+"/CSQTT-universal.apk", csqttAPK)
	if err != nil {
		return nil, err
	}
	artifact, err := unpackTransportAPK(pkg.Data, asset, filename)
	if err != nil {
		return nil, err
	}
	return []Artifact{artifact}, nil
}

func unpackTransportAPK(pkg []byte, asset portableAsset, name string) (Artifact, error) {
	apk, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return Artifact{}, err
	}
	f, err := apk.Open(asset.name)
	if err != nil {
		return Artifact{}, fmt.Errorf("не найден сервер в официальном APK: %w", err)
	}
	defer f.Close()
	bytes, err := io.ReadAll(io.LimitReader(f, 32<<20))
	if err != nil {
		return Artifact{}, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes)) != asset.digest {
		return Artifact{}, fmt.Errorf("SHA-256 бинарника CSQTT не совпал с манифестом релиза")
	}
	return Artifact{Name: name, Data: bytes, SHA256: asset.digest}, nil
}

const (
	vkSourcePin = "37d6607f5c8f3bc4c84595874c6dafa0f5b1d847"
	vkSourceSHA = "efdebe65951f60fe19b005c93f8256159faa8b83162046ecbbd990d9f0a87893"
)

func randomHex(size int) func() (string, error) {
	return func() (string, error) {
		data := make([]byte, size)
		_, err := rand.Read(data)
		return hex.EncodeToString(data), err
	}
}

func prepareVKTurn(n *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	a := n.AccessForModule("client-wireguard")
	p["IFACE"] = a.Iface
	var ok bool
	p["BACKEND_PORT"], ok = sec.Get(n.Name + "/" + a.Module + "/" + a.Iface + "/port")
	if !ok || !inventory.InterfaceName.MatchString(a.Iface) {
		return nil, fmt.Errorf("VK TURN: сначала установите client-wireguard")
	}
	if err := transportPort(p, "PORT", "56000"); err != nil {
		return nil, err
	}
	if p["PORT"] == p["BACKEND_PORT"] {
		return nil, fmt.Errorf("VK TURN и WireGuard используют один UDP-порт")
	}
	p["SOURCE_PIN"] = vkSourcePin
	var err error
	changed := false
	p["WRAP_KEY"], err = ensure(sec, n.Name+"/vk-turn-proxy/wrap_key", randomHex(32), &changed)
	if err == nil && changed {
		err = sec.Save()
	}
	return p, err
}

func vkTurnArtifact(ctx context.Context, cl *remote.Client) ([]Artifact, error) {
	r, err := cl.Run(ctx, "test -x /usr/local/lib/vpnkit/vk-turn-server && cat /usr/local/lib/vpnkit/vk-turn-source || true")
	if err != nil {
		return nil, err
	}
	if r.Code == 0 && strings.TrimSpace(r.Stdout) == vkSourcePin {
		return nil, nil
	}
	src, err := downloadArtifact(ctx, "vk-turn-source.tar.gz",
		"https://codeload.github.com/cacggghp/vk-turn-proxy/tar.gz/"+vkSourcePin, vkSourceSHA)
	if err != nil {
		return nil, err
	}
	return []Artifact{src}, nil
}

func prepareExitSocks(exit *inventory.Node, p map[string]string, sec *secrets.Store) (map[string]string, error) {
	network := Params(exit, "cascade-awg0")["TUNNEL_NET"]
	if network == "" {
		network = "10.100.0.0/30"
	}
	ip, cidr, err := net.ParseCIDR(network)
	if err != nil || ip.To4() == nil {
		return nil, fmt.Errorf("SOCKS5: неверная служебная сеть каскада")
	}
	mask, _ := cidr.Mask.Size()
	if mask > 30 {
		return nil, fmt.Errorf("SOCKS5: в служебной сети нет двух адресов")
	}
	b := cidr.IP.To4()
	p["BIND_IP"], p["ENTRY_TUN"] = net.IPv4(b[0], b[1], b[2], b[3]+1).String(),
		net.IPv4(b[0], b[1], b[2], b[3]+2).String()
	if err := transportPort(p, "PORT", "1080"); err != nil {
		return nil, err
	}
	changed := false
	p["SOCKS_PASSWORD"], err = ensure(sec, exit.Name+"/exit-socks/password", randomHex(24), &changed)
	if err == nil && changed {
		err = sec.Save()
	}
	return p, err
}
