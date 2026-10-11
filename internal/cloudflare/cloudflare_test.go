package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Поддельный Cloudflare: зона example.org; free.example.org нет, same — тот же адрес,
// other — чужой адрес, proxied — наш адрес, но через прокси.
func fake(t *testing.T, posted *[]string) *httptest.Server {
	recs := map[string][]dnsRecord{
		"same.example.org":    {{ID: "1", Type: "A", Name: "same.example.org", Content: "1.2.3.4"}},
		"other.example.org":   {{ID: "2", Type: "A", Name: "other.example.org", Content: "9.9.9.9"}},
		"proxied.example.org": {{ID: "3", Type: "A", Name: "proxied.example.org", Content: "1.2.3.4", Proxied: true}},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := func(v any) {
			b, _ := json.Marshal(v)
			w.Write([]byte(`{"success":true,"errors":[],"result":` + string(b) + `}`))
		}
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(403)
			w.Write([]byte(`{"success":false,"errors":[{"message":"Authentication error"}],"result":null}`))
			return
		}
		switch {
		case r.URL.Path == "/zones":
			if r.URL.Query().Get("name") == "example.org" {
				ok([]map[string]string{{"id": "z1", "name": "example.org"}})
			} else {
				ok([]any{})
			}
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/dns_records"):
			ok(recs[r.URL.Query().Get("name")])
		case r.Method == "POST":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			if b["proxied"] != false {
				t.Error("запись создаётся с проксированием")
			}
			*posted = append(*posted, b["name"].(string))
			ok(map[string]string{"id": "new"})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestEnsure(t *testing.T) {
	var posted []string
	s := fake(t, &posted)
	defer s.Close()
	API = s.URL
	recs, err := Ensure(context.Background(), "good", "example.org", map[string]string{
		"free.example.org": "1.2.3.4", "same.example.org": "1.2.3.4",
		"other.example.org": "1.2.3.4", "proxied.example.org": "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range recs {
		got[r.Name] = r.Status
	}
	want := map[string]string{"free.example.org": "created", "same.example.org": "exists",
		"other.example.org": "conflict", "proxied.example.org": "conflict"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, ждали %s", k, got[k], v)
		}
	}
	if len(posted) != 1 || posted[0] != "free.example.org" {
		t.Errorf("создано: %v — трогать можно только недостающее", posted)
	}
	if _, err := Ensure(context.Background(), "bad", "example.org", nil); err == nil || !strings.Contains(err.Error(), "токен") {
		t.Errorf("плохой токен: %v", err)
	}
	if _, err := Ensure(context.Background(), "good", "nope.org", nil); err == nil || !strings.Contains(err.Error(), "зоны") {
		t.Errorf("нет зоны: %v", err)
	}
}
