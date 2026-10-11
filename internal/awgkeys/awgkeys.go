// Package awgkeys — ключи WireGuard/AmneziaWG и параметры обфускации AmneziaWG 3.1.
//
// Ограничения взяты из модуля ядра amneziawg (netlink.c, v3.1.20260812):
//   - при HeaderProtectionKey все S1..S4 >= HEADER_PROTECTION_NONCE_SIZE (12);
//   - диапазоны H1..H4 не пересекаются;
//   - (классика WireGuard) итоговые размеры пакетов рукопожатия различаются:
//     148+S1, 92+S2, 64+S3, 32+S4.
package awgkeys

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// Keypair — пара ключей в base64 (формат WireGuard).
type Keypair struct{ Private, Public string }

// NewKeypair генерирует пару ключей Curve25519 (как `awg genkey | awg pubkey`).
func NewKeypair() (Keypair, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return Keypair{}, err
	}
	k[0] &= 248
	k[31] = (k[31] & 127) | 64
	pub, err := curve25519.X25519(k[:], curve25519.Basepoint)
	if err != nil {
		return Keypair{}, err
	}
	return Keypair{base64.StdEncoding.EncodeToString(k[:]), base64.StdEncoding.EncodeToString(pub)}, nil
}

// PublicOf вычисляет открытый ключ по закрытому.
func PublicOf(private string) (string, error) {
	k, err := base64.StdEncoding.DecodeString(private)
	if err != nil || len(k) != 32 {
		return "", fmt.Errorf("закрытый ключ: нужен base64 32 байт")
	}
	pub, err := curve25519.X25519(k, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// NewSymmetric — 32 случайных байта в base64: PresharedKey или HeaderProtectionKey
// (оба в amneziawg-tools разбираются как ключ WireGuard, parse_key).
func NewSymmetric() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k[:]), nil
}

func rnd(min, max int64) int64 { // включительно
	n, err := rand.Int(rand.Reader, big.NewInt(max-min+1))
	if err != nil {
		panic(err)
	}
	return min + n.Int64()
}

// Obfs — параметры обфускации AmneziaWG (одинаковы на обоих концах, кроме Jc/Jmin/Jmax).
type Obfs struct {
	Jc, Jmin, Jmax int
	S1, S2, S3, S4 int
	H              [4][2]uint32 // диапазоны H1..H4
	I              []string     // I1..I4 — имитация протоколов
}

// Шаблоны имитации (для межсерверного туннеля): STUN-запрос/ответ/индикация и RTP.
var cascadeI = []string{
	"<b 0x000100002112a442><r 12>",
	"<b 0x0101000c2112a442><r 12><b 0x002000080001><r 6>",
	"<b 0x0011000c2112a442><r 12><b 0x80220008><rc 8>",
	"<b 0x8060><r 2><t><r 28>",
}

// ClientI — шаблоны имитации для клиентского входа : DTLS и RTCP.
var ClientI = []string{
	"<b 0x16fefd0000000000000000><r 2><b 0x01><r 67>",
	"<b 0x80c90001><r 4>",
}

// NewObfs генерирует набор для межсерверного туннеля (имитация STUN/RTP).
func NewObfs() Obfs { return NewObfsWith(cascadeI) }

// NewObfsWith генерирует допустимый набор параметров с HeaderProtectionKey и заданными I1..In.
func NewObfsWith(templates []string) Obfs {
	var o Obfs
	o.Jc = int(rnd(4, 8))
	o.Jmin = int(rnd(32, 64))
	o.Jmax = o.Jmin + int(rnd(48, 96))
	for i := 0; ; i++ {
		o.S1, o.S2, o.S3, o.S4 = int(rnd(15, 120)), int(rnd(15, 120)), int(rnd(12, 40)), int(rnd(12, 32))
		if o.validS() == nil {
			break
		}
		if i > 1000 {
			panic("awgkeys: не удалось подобрать S1..S4")
		}
	}
	// четыре непересекающихся диапазона по ~150–250 млн в [1e8, 4.2e9]
	starts := []int64{}
	for len(starts) < 4 {
		s := rnd(100_000_000, 3_900_000_000)
		ok := true
		for _, x := range starts {
			if s > x-300_000_000 && s < x+300_000_000 {
				ok = false
			}
		}
		if ok {
			starts = append(starts, s)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	for i, s := range starts {
		o.H[i] = [2]uint32{uint32(s), uint32(s + rnd(150_000_000, 250_000_000))}
	}
	o.I = templates
	return o
}

// Valid проверяет ограничения модуля ядра.
func (o Obfs) Valid() error {
	if o.Jc < 1 || o.Jmin < 1 || o.Jmax < o.Jmin || o.Jmax > 1200 {
		return fmt.Errorf("Jc/Jmin/Jmax вне допустимого: %d/%d/%d", o.Jc, o.Jmin, o.Jmax)
	}
	if err := o.validS(); err != nil {
		return err
	}
	return o.validH()
}

// validS — ограничения на S1..S4 (без H: нужна при подборе S, пока H ещё не заданы).
func (o Obfs) validS() error {
	for i, s := range []int{o.S1, o.S2, o.S3, o.S4} {
		if s < 12 {
			return fmt.Errorf("S%d=%d < 12 (нужно для HeaderProtectionKey)", i+1, s)
		}
	}
	sizes := map[int]bool{}
	for _, sz := range []int{148 + o.S1, 92 + o.S2, 64 + o.S3, 32 + o.S4} {
		if sizes[sz] {
			return fmt.Errorf("совпали размеры пакетов рукопожатия (%d)", sz)
		}
		sizes[sz] = true
	}
	return nil
}

// validH — диапазоны H1..H4: не пересекаются и не задевают штатные типы 1..4.
func (o Obfs) validH() error {
	for i := 0; i < 4; i++ {
		if o.H[i][0] != 0 && o.H[i][0] < 5 {
			return fmt.Errorf("H%d начинается с %d — пересечение со штатными типами WireGuard 1..4", i+1, o.H[i][0])
		}
		if o.H[i][1] < o.H[i][0] {
			return fmt.Errorf("H%d: конец меньше начала", i+1)
		}
		for j := i + 1; j < 4; j++ {
			if o.H[i][0] <= o.H[j][1] && o.H[j][0] <= o.H[i][1] {
				return fmt.Errorf("H%d и H%d пересекаются", i+1, j+1)
			}
		}
	}
	return nil
}

// Lines — строки для секции [Interface] конфига awg-quick.
func (o Obfs) Lines() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Jc = %d\nJmin = %d\nJmax = %d\n", o.Jc, o.Jmin, o.Jmax)
	fmt.Fprintf(&b, "S1 = %d\nS2 = %d\nS3 = %d\nS4 = %d\n", o.S1, o.S2, o.S3, o.S4)
	for i, h := range o.H {
		fmt.Fprintf(&b, "H%d = %d-%d\n", i+1, h[0], h[1])
	}
	for i, s := range o.I {
		fmt.Fprintf(&b, "I%d = %s\n", i+1, s)
	}
	return b.String()
}
