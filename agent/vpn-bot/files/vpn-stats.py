#!/usr/bin/env python3
"""Сборщик статистики трафика клиентов AmneziaWG (таймер раз в 5 минут).

Счётчики `awg show` обнуляются при перезапуске интерфейса, поэтому копится разница:
снимаем показания, вычитаем предыдущие, кладём приращение в SQLite.
Имя клиента берётся из строки `# name:` перед [Peer] — так их пишет Крот.
База: /var/lib/vpnkit/stats.db (только стандартная библиотека).
"""
import glob
import os
import sqlite3
import subprocess
import time

DB = "/var/lib/vpnkit/stats.db"
CONFS = sorted(glob.glob("/etc/amnezia/amneziawg/*.conf"))
KEEP_DAYS = 90


def run(args):
    try:
        p = subprocess.run(args, capture_output=True, text=True, timeout=20)
        return p.returncode, p.stdout.strip()
    except Exception:
        return 1, ""


def peer_names():
    names = {}
    for path in CONFS:
        name = None
        for line in open(path):
            s = line.strip()
            if s.startswith("# name:"):
                name = s.split(":", 1)[1].strip()
            elif s.startswith("PublicKey") and name:
                names[s.split("=", 1)[1].strip()] = name
                name = None
    return names


def main():
    os.makedirs(os.path.dirname(DB), exist_ok=True)
    db = sqlite3.connect(DB)
    db.executescript("""
    CREATE TABLE IF NOT EXISTS traffic (ts INTEGER NOT NULL, peer TEXT NOT NULL, rx INTEGER NOT NULL, tx INTEGER NOT NULL);
    CREATE INDEX IF NOT EXISTS traffic_ts ON traffic(ts);
    CREATE TABLE IF NOT EXISTS last_peer (peer TEXT PRIMARY KEY, rx INTEGER NOT NULL, tx INTEGER NOT NULL);
    """)
    names = peer_names()
    prev = {r[0]: (r[1], r[2]) for r in db.execute("SELECT peer,rx,tx FROM last_peer")}
    now = int(time.time())
    for path in CONFS:
        iface = os.path.basename(path)[:-5]
        rc, out = run(["awg", "show", iface, "dump"])
        if rc:
            continue
        for line in out.splitlines()[1:]:  # первая строка — сам интерфейс
            f = line.split("\t")
            if len(f) < 8:
                continue
            pub, rx, tx = f[0], int(f[5] or 0), int(f[6] or 0)
            if pub not in names:  # служебные пиры (каскад) не считаем
                continue
            prx, ptx = prev.get(pub, (0, 0))
            drx = rx - prx if rx >= prx else rx  # счётчик сбросился — берём как есть
            dtx = tx - ptx if tx >= ptx else tx
            if drx or dtx:
                db.execute("INSERT INTO traffic(ts,peer,rx,tx) VALUES(?,?,?,?)", (now, names[pub], drx, dtx))
            db.execute("INSERT INTO last_peer(peer,rx,tx) VALUES(?,?,?) ON CONFLICT(peer) DO UPDATE SET rx=?,tx=?",
                       (pub, rx, tx, rx, tx))
    db.execute("DELETE FROM traffic WHERE ts < ?", (now - KEEP_DAYS * 86400,))
    db.commit()
    db.close()


if __name__ == "__main__":
    main()
