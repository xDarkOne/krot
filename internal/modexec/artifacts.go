package modexec

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xDarkOne/krot/internal/remote"
)

// Artifact — двоичный файл для узла (пакет), скачанный и проверенный пультом.
type Artifact struct {
	Name   string
	Data   []byte
	SHA256 string
}

// artifacts подготавливает пакеты резерва до начала изменений на сервере.
// Вход может не иметь доступа к GitHub, поэтому загрузку делает пульт.
func artifacts(ctx context.Context, cl *remote.Client, module string, params map[string]string) ([]Artifact, error) {
	if module != "cascade-awg0" || params["REALITY_NAME"] == "" {
		return nil, nil
	}
	r, err := cl.Run(ctx, `dpkg --print-architecture
/usr/local/lib/vpnkit/cascade/xray version 2>/dev/null | awk 'NR==1{print $2} END{if (!NR) print ""}'
/usr/local/lib/vpnkit/cascade/sing-box version 2>/dev/null | awk 'NR==1{print $3} END{if (!NR) print ""}'
`)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
	xasset := map[string]string{"amd64": "Xray-linux-64.zip", "arm64": "Xray-linux-arm64-v8a.zip"}[lines[0]]
	if r.Code != 0 || xasset == "" {
		return nil, fmt.Errorf("резерв каскада: архитектура узла не поддерживается")
	}
	arch := lines[0]
	hasVersion := func(index int, param string) bool {
		return len(lines) > index && lines[index] == params[param]
	}
	suffix := "_SHA256_" + strings.ToUpper(arch)
	var out []Artifact
	if !hasVersion(1, "XRAY_VERSION") {
		url := "https://github.com/XTLS/Xray-core/releases/download/v" + params["XRAY_VERSION"] + "/" + xasset
		a, err := downloadArtifact(ctx, "cascade-xray.zip", url, params["XRAY"+suffix])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if params["SIDE"] == "client" && !hasVersion(2, "SINGBOX_VERSION") {
		v := params["SINGBOX_VERSION"]
		url := "https://github.com/SagerNet/sing-box/releases/download/v" + v + "/sing-box_" + v + "_linux_" + arch + ".deb"
		a, err := downloadArtifact(ctx, "cascade-sing-box.deb", url, params["SINGBOX"+suffix])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func downloadArtifact(ctx context.Context, name, url, digest string) (Artifact, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Artifact{}, err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return Artifact{}, fmt.Errorf("скачать %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Artifact{}, fmt.Errorf("скачать %s: HTTP %d", name, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Artifact{}, fmt.Errorf("прочитать %s: %w", name, err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return Artifact{}, fmt.Errorf("SHA-256 %s не совпал с пином", name)
	}
	return Artifact{Name: name, Data: data, SHA256: digest}, nil
}
