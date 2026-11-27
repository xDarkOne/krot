package modexec

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/xDarkOne/krot/internal/inventory"
	"github.com/xDarkOne/krot/internal/secrets"
)

var realityDNSLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validRealityName(name string) bool {
	if len(name) > 253 || !strings.Contains(name, ".") || net.ParseIP(name) != nil {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if !realityDNSLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// ValidateCascadeReserve проверяет настройки мастера до записи inventory и начала установки.
func ValidateCascadeReserve(exit *inventory.Node) error {
	return cascadeReserveParams(exit, map[string]string{"PORT": "1"})
}

func cascadeReserveParams(exit *inventory.Node, p map[string]string) error {
	cfg := Params(exit, "cascade-awg0")
	// Резервы согласуются только по настройкам выхода.
	delete(p, "ENDPOINT_V6")
	delete(p, "REALITY_NAME")
	delete(p, "REALITY_PORT")
	if addr := cfg["EXIT_IPV6"]; addr != "" {
		if host := net.ParseIP(exit.Host); host != nil && host.To4() == nil {
			return fmt.Errorf("cascade-awg0: нужен IPv4 в поле host выхода; резервный IPv6 задаётся отдельно")
		}
		ip := net.ParseIP(addr)
		if ip == nil || ip.To4() != nil || ip.IsPrivate() || !ip.IsGlobalUnicast() {
			return fmt.Errorf("cascade-awg0.exit_ipv6: нужен глобальный IPv6-адрес выхода")
		}
		p["ENDPOINT_V6"] = net.JoinHostPort(ip.String(), p["PORT"])
	}

	name := cfg["REALITY_SERVER_NAME"]
	if name == "" {
		return nil
	}
	if !validRealityName(name) {
		return fmt.Errorf("cascade-awg0.reality_server_name: нужно DNS-имя TLS-сайта")
	}
	if strings.EqualFold(name, exit.Host) {
		return fmt.Errorf("cascade-awg0.reality_server_name: имя должно отличаться от адреса выхода")
	}
	port := cfg["REALITY_PORT"]
	if port == "" {
		port = "24443"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("cascade-awg0.reality_port: нужен TCP-порт 1–65535")
	}
	p["REALITY_NAME"], p["REALITY_PORT"] = name, strconv.Itoa(number)
	return nil
}

func prepareCascadeReserve(exit *inventory.Node, p map[string]string, sec *secrets.Store, ns string, changed *bool) error {
	if err := cascadeReserveParams(exit, p); err != nil || p["REALITY_NAME"] == "" {
		return err
	}
	p["XRAY_VERSION"], p["XRAY_SHA256_AMD64"], p["XRAY_SHA256_ARM64"] = XrayVersion, xraySHA256AMD64, xraySHA256ARM64
	p["SINGBOX_VERSION"], p["SINGBOX_SHA256_AMD64"], p["SINGBOX_SHA256_ARM64"] = SingBoxVersion, singboxSHA256AMD, singboxSHA256ARM

	private, err := ensure(sec, ns+"reality_private", func() (string, error) {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(key.Bytes()), nil
	}, changed)
	if err != nil {
		return err
	}
	raw, err := base64.RawURLEncoding.DecodeString(private)
	if err != nil {
		return fmt.Errorf("повреждён ключ Reality в хранилище")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return fmt.Errorf("повреждён ключ Reality в хранилище")
	}
	if p["SIDE"] == "server" {
		p["REALITY_PRIVATE"] = private
	} else {
		p["REALITY_PUBLIC"] = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	}
	p["REALITY_UUID"], err = ensure(sec, ns+"reality_uuid", func() (string, error) {
		var uuid [16]byte
		if _, err := rand.Read(uuid[:]); err != nil {
			return "", err
		}
		uuid[6] = (uuid[6] & 0x0f) | 0x40
		uuid[8] = (uuid[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
	}, changed)
	if err != nil {
		return err
	}
	p["REALITY_SHORT_ID"], err = ensure(sec, ns+"reality_short_id", func() (string, error) {
		var id [8]byte
		_, err := rand.Read(id[:])
		return hex.EncodeToString(id[:]), err
	}, changed)
	return err
}
