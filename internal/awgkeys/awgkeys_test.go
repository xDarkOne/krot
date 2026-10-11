package awgkeys

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestKeypair(t *testing.T) {
	kp, err := NewKeypair()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := PublicOf(kp.Private)
	if err != nil || pub != kp.Public {
		t.Fatalf("PublicOf не совпал: %v %s %s", err, pub, kp.Public)
	}
	raw, _ := base64.StdEncoding.DecodeString(kp.Private)
	if len(raw) != 32 || raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
		t.Fatal("закрытый ключ не «зажат» по правилам Curve25519")
	}
}

func TestObfsAlwaysValid(t *testing.T) {
	for i := 0; i < 2000; i++ {
		o := NewObfs()
		if err := o.Valid(); err != nil {
			t.Fatalf("прогон %d: %v (%+v)", i, err, o)
		}
		if !strings.Contains(o.Lines(), "I4 = ") || !strings.Contains(o.Lines(), "H4 = ") {
			t.Fatal("в строках нет H4/I4")
		}
	}
}

func TestValidRejects(t *testing.T) {
	o := NewObfs()
	o.S1 = 11
	if o.Valid() == nil {
		t.Fatal("S1 < 12 должен отвергаться")
	}
	o = NewObfs()
	o.S2 = o.S1 + 56 // 148+S1 == 92+S2
	if o.Valid() == nil {
		t.Fatal("совпадение размеров пакетов должно отвергаться")
	}
	o = NewObfs()
	o.H[1] = o.H[0]
	if o.Valid() == nil {
		t.Fatal("пересечение H должно отвергаться")
	}
}
