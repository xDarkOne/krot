package agent

import (
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var pipeGrepQ = regexp.MustCompile(`\|\s*grep\s+(-[a-zA-Z]*q|--quiet)`)

// Правки скриптов через Python не раз превращали «\1» и «\n» в управляющие символы или
// настоящие переводы строк — шаг потом падал уже на узле. Ловим это до коммита.
func TestScriptsClean(t *testing.T) {
	bash, _ := exec.LookPath("bash")
	py, _ := exec.LookPath("python3")
	if py == "" {
		py, _ = exec.LookPath("python")
	}
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := fs.ReadFile(files, p)
		for i, r := range string(b) {
			if r < 0x20 && r != '\n' && r != '\t' {
				t.Errorf("%s: управляющий символ %#x на позиции %d", p, r, i)
				break
			}
			if r == '\r' {
				t.Errorf("%s: CRLF", p)
				break
			}
		}
		// P-47: `cmd | grep -q` под pipefail — grep выходит на первом совпадении, cmd получает
		// SIGPIPE, и конвейер «падает», хотя совпадение есть. Только grep -q … <<<"$(cmd)".
		if strings.HasSuffix(p, ".sh") {
			for i, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(l), "#") {
					continue
				}
				if pipeGrepQ.MatchString(l) {
					t.Errorf("%s:%d: «| grep -q» под pipefail даёт ложные провалы (P-47) — grep -q … <<<\"$(cmd)\"", p, i+1)
				}
			}
		}
		switch {
		case strings.HasSuffix(p, ".sh") && bash != "":
			c := exec.Command(bash, "-n")
			c.Stdin = strings.NewReader(string(b))
			if out, err := c.CombinedOutput(); err != nil {
				t.Errorf("%s: bash -n: %v\n%s", p, err, out)
			}
		case strings.HasSuffix(p, ".py") && py != "":
			c := exec.Command(py, "-c", "import ast,sys;ast.parse(sys.stdin.buffer.read())")
			c.Stdin = strings.NewReader(string(b))
			if out, err := c.CombinedOutput(); err != nil {
				t.Errorf("%s: синтаксис Python: %v\n%s", p, err, out)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
