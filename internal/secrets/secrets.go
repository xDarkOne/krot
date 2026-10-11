// Package secrets — хранилище секретов пульта: один файл, зашифрованный age
// по паролю (scrypt). Ключи вида "<узел>/<модуль>/<имя>", например
// "eu/monitoring/token". На диске открытым текстом секреты не лежат никогда.
package secrets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"filippo.io/age"
	"golang.org/x/term"
)

// PassEnv — переменная окружения с паролем хранилища (для скриптов).
const PassEnv = "VPNKIT_SECRETS_PASS"

// Store — расшифрованное хранилище в памяти. Шифруется либо ключом X25519 из
// ~/.vpnkit/age.key (режим ключа: как SSH-ключ, без пароля), либо паролем (scrypt).
type Store struct {
	path   string
	pass   string
	id     *age.X25519Identity
	values map[string]string
}

// KeyPath — ключ-файл age пульта (~/.vpnkit/age.key или $VPNKIT_HOME/age.key).
func KeyPath() string {
	if d := os.Getenv("VPNKIT_HOME"); d != "" {
		return filepath.Join(d, "age.key")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".vpnkit", "age.key")
}

// LoadKey читает ключ пульта; nil, если ключ-файла нет.
func LoadKey() (*age.X25519Identity, error) {
	b, err := os.ReadFile(KeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids, err := age.ParseIdentities(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", KeyPath(), err)
	}
	for _, i := range ids {
		if x, ok := i.(*age.X25519Identity); ok {
			return x, nil
		}
	}
	return nil, fmt.Errorf("%s: нет ключа X25519", KeyPath())
}

// CreateKey создаёт ключ-файл пульта (права 600). Отказ, если он уже есть.
func CreateKey() (*age.X25519Identity, error) {
	if _, err := os.Stat(KeyPath()); err == nil {
		return nil, fmt.Errorf("%s уже есть", KeyPath())
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(KeyPath()), 0o700); err != nil {
		return nil, err
	}
	body := "# ключ пульта vpnkit: расшифровка secrets.age и резервных копий узлов\n" +
		"# публичная часть: " + id.Recipient().String() + "\n" + id.String() + "\n"
	return id, os.WriteFile(KeyPath(), []byte(body), 0o600)
}

// DefaultPath — secrets.age рядом с inventory.
func DefaultPath(inventoryPath string) string {
	return filepath.Join(filepath.Dir(inventoryPath), "secrets.age")
}

// Exists сообщает, есть ли файл хранилища.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// PassPrompt, если задан, заменяет запрос пароля в терминале (приложение спрашивает в окне).
var PassPrompt func(prompt string) (string, error)

func askPass(prompt string) (string, error) {
	if p := os.Getenv(PassEnv); p != "" {
		return p, nil
	}
	if PassPrompt != nil {
		return PassPrompt(prompt)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("нет терминала для пароля хранилища — задайте %s", PassEnv)
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

// CreateWithKey заводит пустое хранилище, зашифрованное ключом пульта.
func CreateWithKey(path string, id *age.X25519Identity) (*Store, error) {
	if Exists(path) {
		return nil, fmt.Errorf("%s уже есть", path)
	}
	s := &Store{path: path, id: id, values: map[string]string{}}
	return s, s.Save()
}

// Create заводит новое пустое хранилище (пароль спрашивается дважды).
func Create(path string) (*Store, error) {
	if Exists(path) {
		return nil, fmt.Errorf("%s уже есть", path)
	}
	p1, err := askPass("Новый пароль хранилища секретов: ")
	if err != nil {
		return nil, err
	}
	if os.Getenv(PassEnv) == "" {
		p2, err := askPass("Ещё раз: ")
		if err != nil {
			return nil, err
		}
		if p1 != p2 {
			return nil, errors.New("пароли не совпали")
		}
	}
	if len(p1) < 12 {
		return nil, errors.New("пароль короче 12 символов")
	}
	s := &Store{path: path, pass: p1, values: map[string]string{}}
	return s, s.Save()
}

// Open расшифровывает хранилище. Пустой путь или отсутствующий файл —
// пустое хранилище только для чтения (без пароля).
func Open(path string) (*Store, error) {
	if !Exists(path) {
		return &Store{path: path, values: map[string]string{}}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, values: map[string]string{}}
	var ident age.Identity
	if key, kerr := LoadKey(); kerr != nil {
		return nil, kerr
	} else if key != nil && !bytes.Contains(b[:min(len(b), 200)], []byte("scrypt")) {
		s.id, ident = key, key
	} else {
		pass, err := askPass("Пароль хранилища секретов: ")
		if err != nil {
			return nil, err
		}
		sid, err := age.NewScryptIdentity(pass)
		if err != nil {
			return nil, err
		}
		s.pass, ident = pass, sid
	}
	r, err := age.Decrypt(bytes.NewReader(b), ident)
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать %s (неверный пароль?): %w", path, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(plain, &s.values); err != nil {
		return nil, fmt.Errorf("%s: повреждено содержимое: %w", path, err)
	}
	return s, nil
}

// Save шифрует и атомарно записывает хранилище (права 600).
func (s *Store) Save() error {
	var rcpt age.Recipient
	switch {
	case s.id != nil:
		rcpt = s.id.Recipient()
	case s.pass != "":
		sr, err := age.NewScryptRecipient(s.pass)
		if err != nil {
			return err
		}
		rcpt = sr
	default:
		return errors.New("хранилище открыто без ключа и пароля — запись невозможна")
	}
	plain, err := json.Marshal(s.values)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rcpt)
	if err != nil {
		return err
	}
	if _, err := w.Write(plain); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	tmp := s.path + ".new"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get возвращает секрет.
func (s *Store) Get(key string) (string, bool) {
	v, ok := s.values[key]
	return v, ok
}

// Set задаёт секрет (без записи на диск — вызовите Save).
func (s *Store) Set(key, value string) error {
	if strings.Count(key, "/") < 2 || strings.Contains(key, "//") || strings.HasSuffix(key, "/") {
		return fmt.Errorf("ключ %q: нужен вид <узел>/<модуль>/<имя>[/…]", key)
	}
	s.values[key] = value
	return nil
}

// Delete удаляет секрет.
func (s *Store) Delete(key string) bool {
	_, ok := s.values[key]
	delete(s.values, key)
	return ok
}

// Keys — имена секретов (значения не показываются).
func (s *Store) Keys() []string {
	var k []string
	for x := range s.values {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// ForModule — секреты модуля узла как переменные для подстановки: имя → значение.
func (s *Store) ForModule(node, module string) map[string]string {
	out := map[string]string{}
	prefix := node + "/" + module + "/"
	for k, v := range s.values {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out
}

// Values — все значения (для маскировки вывода).
func (s *Store) Values() []string {
	var v []string
	for _, x := range s.values {
		if len(x) >= 4 {
			v = append(v, x)
		}
	}
	return v
}

// Mask заменяет в тексте все известные секреты на <secret>.
func Mask(text string, values []string) string {
	for _, v := range values {
		text = strings.ReplaceAll(text, v, "<secret>")
	}
	return text
}
