"""Read Linux uptime, interface counters and the existing Krot traffic database."""
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import time


def interface_counters():
    try:
        for family in ("-4", "-6"):
            routes = subprocess.check_output(
                ["ip", family, "route", "show", "default"], text=True, timeout=2
            ).splitlines()
            for route in routes:
                fields = route.split()
                if "dev" in fields:
                    name = fields[fields.index("dev") + 1]
                    stats = Path("/sys/class/net") / name / "statistics"
                    return {"interface": name, "rx": int((stats / "rx_bytes").read_text()),
                            "tx": int((stats / "tx_bytes").read_text())}
    except (OSError, subprocess.SubprocessError, ValueError, IndexError):
        pass
    return {}


def traffic_history(db_path, days, now):
    if not db_path.is_file():
        return {}
    step = 3600 if days == 1 else 86400
    count = 24 if days == 1 else days
    latest = now // step
    first = latest - count + 1
    buckets = {idx: {} for idx in range(first, latest + 1)}
    try:
        with closing(sqlite3.connect(db_path.as_uri() + "?mode=ro", uri=True, timeout=1)) as db:
            tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
            if not tables.intersection({"traffic", "protocol_traffic"}):
                return {}
            if "traffic" in tables:
                for bucket, total in db.execute(
                    "SELECT ts / ?, SUM(rx + tx) FROM traffic WHERE ts BETWEEN ? AND ? GROUP BY 1",
                    (step, first * step, now)
                ):
                    buckets[bucket]["awg"] = total
            if "protocol_traffic" in tables:
                for bucket, protocol, total in db.execute(
                    "SELECT ts / ?, protocol, SUM(rx + tx) FROM protocol_traffic WHERE ts BETWEEN ? AND ? GROUP BY 1, 2",
                    (step, first * step, now)
                ):
                    if protocol in {"xhttp", "pq", "hy2"}:
                        buckets[bucket][protocol] = total
    except sqlite3.Error:
        return {"history_error": True}
    return {"has_history": True, "traffic": [{"at": idx * step, "bytes": values} for idx, values in buckets.items()]}


if __name__ == "__main__":
    uptime = int(float(Path("/proc/uptime").read_text().split()[0]))
    boot = next(int(line.split()[1]) for line in Path("/proc/stat").read_text().splitlines() if line.startswith("btime "))
    result = {"uptime_seconds": uptime, "booted_at": boot, **interface_counters()}
    if sys.argv[2] == "1":
        result.update(traffic_history(Path("/var/lib/vpnkit/stats.db"), int(sys.argv[1]), int(time.time())))
    print(json.dumps(result))
