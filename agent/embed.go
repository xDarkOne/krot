// Package agent встраивает скрипты, которые пульт выполняет на узлах:
// lib.sh (общая библиотека) и <модуль>/{preflight,install,verify,rollback}.sh.
package agent

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

//go:embed lib.sh */*.sh */files/*
var files embed.FS

// Lib — общая библиотека агента.
func Lib() string {
	b, _ := files.ReadFile("lib.sh")
	return string(b)
}

// Step возвращает скрипт шага модуля; ok=false, если шага нет.
func Step(module, step string) (string, bool) {
	b, err := fs.ReadFile(files, module+"/"+step+".sh")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Files — файлы модуля из agent/<модуль>/files/ плюс общие из agent/common/files/
// (имя → содержимое): пульт кладёт их в $VK_RUN/src/ на узле перед шагом install.
// Общие — то, чем владеют несколько модулей сразу (vpnkit-gen.py).
func Files(module string) map[string]string {
	out := map[string]string{}
	for _, dir := range []string{"common", module} {
		ents, err := fs.ReadDir(files, dir+"/files")
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			b, err := fs.ReadFile(files, path.Join(dir, "files", e.Name()))
			if err == nil {
				out[e.Name()] = string(b)
			}
		}
	}
	return out
}

// Hash — отпечаток кода модуля: lib.sh, его шаги, его файлы и общие файлы, плюс extra
// (закреплённые версии бинарников). Пишется в метку узла при установке; несовпадение с
// текущим пультом значит «на узле старая версия модуля — доступно обновление».
func Hash(module, extra string) string {
	h := sha256.New()
	add := func(name string) {
		b, err := fs.ReadFile(files, name)
		if err != nil {
			return
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(b))
		h.Write(b)
	}
	add("lib.sh")
	var names []string
	for _, dir := range []string{module, module + "/files", "common/files"} {
		ents, _ := fs.ReadDir(files, dir)
		for _, e := range ents {
			if !e.IsDir() {
				names = append(names, path.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(names)
	for _, n := range names {
		add(n)
	}
	fmt.Fprintf(h, "extra\x00%s", extra)
	return hex.EncodeToString(h.Sum(nil))[:12]
}
