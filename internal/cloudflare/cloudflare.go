// Package cloudflare — A-записи для мастера через API Cloudflare (токен с правом
// Zone.DNS:Edit только на эту зону). Политика: только СОЗДАВАТЬ недостающее.
// Существующую запись с другим адресом или с проксированием не трогаем — на имени может
// жить чужой сервис; сообщаем о конфликте, человек выбирает другое имя.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API — адрес API (подменяется в тестах).
var API = "https://api.cloudflare.com/client/v4"

type client struct {
	bearer string
	http   *http.Client
}

type envelope struct {
	Success bool                       `json:"success"`
	Errors  []struct{ Message string } `json:"errors"`
	Result  json.RawMessage            `json:"result"`
}

func (c client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, API+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Content-Type", "application/json")
	r, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Cloudflare не ответил: %w", err)
	}
	defer r.Body.Close()
	var e envelope
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&e); err != nil {
		return fmt.Errorf("Cloudflare: ответ не разобрать (HTTP %d)", r.StatusCode)
	}
	if !e.Success {
		var m []string
		for _, x := range e.Errors {
			m = append(m, x.Message)
		}
		msg := strings.Join(m, "; ")
		if r.StatusCode == 401 || r.StatusCode == 403 || strings.Contains(msg, "Invalid") || strings.Contains(msg, "Authentication") {
			return fmt.Errorf("Cloudflare не принял токен или у него нет прав на DNS этой зоны (%s)", msg)
		}
		return fmt.Errorf("Cloudflare: %s", msg)
	}
	if out != nil {
		return json.Unmarshal(e.Result, out)
	}
	return nil
}

// Record — итог по одному имени.
type Record struct {
	Name    string `json:"name"`
	Want    string `json:"want"`
	Status  string `json:"status"` // created | exists | conflict
	Problem string `json:"problem,omitempty"`
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// Ensure создаёт A-записи want (имя → IPv4) в зоне domain, не трогая существующие.
func Ensure(ctx context.Context, token, domain string, want map[string]string) ([]Record, error) {
	c := client{bearer: strings.TrimSpace(token), http: &http.Client{Timeout: 20 * time.Second}}
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do(ctx, "GET", "/zones?name="+url.QueryEscape(domain), nil, &zones); err != nil {
		return nil, err
	}
	if len(zones) == 0 {
		return nil, fmt.Errorf("зоны %s в этом аккаунте Cloudflare нет (или токен выдан на другую зону)", domain)
	}
	zone := zones[0].ID
	var out []Record
	for name, ip := range want {
		rec := Record{Name: name, Want: ip}
		var have []dnsRecord
		if err := c.do(ctx, "GET", "/zones/"+zone+"/dns_records?name="+url.QueryEscape(name), nil, &have); err != nil {
			return out, err
		}
		switch {
		case len(have) == 0:
			body := map[string]any{"type": "A", "name": name, "content": ip, "ttl": 1, "proxied": false,
				"comment": "vpnkit"}
			if err := c.do(ctx, "POST", "/zones/"+zone+"/dns_records", body, nil); err != nil {
				return out, fmt.Errorf("%s: %w", name, err)
			}
			rec.Status = "created"
		case len(have) == 1 && have[0].Type == "A" && have[0].Content == ip && !have[0].Proxied:
			rec.Status = "exists"
		default:
			rec.Status = "conflict"
			var d []string
			for _, h := range have {
				p := ""
				if h.Proxied {
					p = " (проксируется)"
				}
				d = append(d, h.Type+" "+h.Content+p)
			}
			rec.Problem = "уже есть запись: " + strings.Join(d, ", ") + " — не трогаю; выберите другой поддомен или исправьте вручную"
		}
		out = append(out, rec)
	}
	return out, nil
}
