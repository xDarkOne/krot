#!/usr/bin/env python3
"""Сквозная проверка MTProto-прокси (FakeTLS, секрет ee…) без клиента Telegram.

Делает то же, что приложение: TLS-рукопожатие с подписью секретом → obfuscated2 →
первый запрос MTProto req_pq_multi к DC. Ответ resPQ может прийти только от самого Telegram:
значит, прокси принял секрет, маскировку и довёз запрос до DC и обратно.

    python3 mtproto-test.py 'tg://proxy?server=…&port=443&secret=ee…' [dc]
Секрет — пропуск в прокси: передавать через аргумент только на своём узле.
"""
import hashlib
import hmac
import os
import socket
import struct
import sys
import time
from urllib.parse import parse_qs, urlsplit

from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes


def ctr(key, iv):
    return Cipher(algorithms.AES(key), modes.CTR(iv)).encryptor()


def ext(t, body):
    return struct.pack(">HH", t, len(body)) + body


def client_hello(domain, session_id):
    sni = domain.encode()
    exts = b"".join([
        ext(0x0000, struct.pack(">HBH", len(sni) + 3, 0, len(sni)) + sni),          # server_name
        ext(0x000a, struct.pack(">H", 6) + bytes.fromhex("001d00170018")),            # groups
        ext(0x000b, b"\x01\x00"),                                                     # ec_point_formats
        ext(0x000d, struct.pack(">H", 8) + bytes.fromhex("0403080404010201")),        # sig algs
        ext(0x0010, struct.pack(">H", 3) + b"\x02h2"),                               # ALPN
        ext(0x002b, b"\x04\x03\x04\x03\x03"),                                         # versions 1.3, 1.2
        ext(0x002d, b"\x01\x01"),                                                     # psk modes
        ext(0x0033, struct.pack(">HHH", 36, 0x001d, 32) + os.urandom(32)),            # key_share x25519
    ])
    ciphers = bytes.fromhex("130113021303c02bc02fc02cc030cca9cca8")
    body = (b"\x03\x03" + b"\x00" * 32 + bytes([len(session_id)]) + session_id +
            struct.pack(">H", len(ciphers)) + ciphers + b"\x01\x00")
    # добить до 517 байт расширением padding — как обычный браузер
    base = 4 + len(body) + 2 + len(exts)
    pad = 517 - 5 - base - 4
    if pad > 0:
        exts += ext(0x0015, b"\x00" * pad)
    hs = b"\x01" + struct.pack(">I", len(body) + 2 + len(exts))[1:] + body + struct.pack(">H", len(exts)) + exts
    return b"\x16\x03\x01" + struct.pack(">H", len(hs)) + hs


def recv_exact(s, n):
    b = b""
    while len(b) < n:
        c = s.recv(n - len(b))
        if not c:
            raise ConnectionError(f"соединение закрыто (получено {len(b)} из {n})")
        b += c
    return b


def recv_record(s):
    h = recv_exact(s, 5)
    return h[0], recv_exact(s, struct.unpack(">H", h[3:5])[0])


def main():
    link = sys.argv[1]
    dc = int(sys.argv[2]) if len(sys.argv) > 2 else 2
    q = parse_qs(urlsplit(link.replace("tg://proxy?", "tg://proxy/?")).query)
    host, port, sec = q["server"][0], int(q["port"][0]), bytes.fromhex(q["secret"][0])
    if sec[0] != 0xEE:
        sys.exit("нужен секрет FakeTLS (ee…)")
    secret, domain = sec[1:17], sec[17:].decode()
    t0 = time.time()
    s = socket.create_connection((host, port), timeout=15)

    # 1. ClientHello с подписью: random = HMAC(секрет, hello с нулевым random) XOR время
    sid = os.urandom(32)
    hello = bytearray(client_hello(domain, sid))
    dig = hmac.new(secret, bytes(hello), hashlib.sha256).digest()
    ts = struct.pack("<I", int(time.time()))
    rnd = dig[:28] + bytes(a ^ b for a, b in zip(dig[28:], ts))
    hello[11:43] = rnd
    s.sendall(bytes(hello))

    # 2. ответ сервера: ServerHello + ChangeCipherSpec + ApplicationData; random подписан секретом
    recs = [recv_record(s) for _ in range(3)]
    if [r[0] for r in recs] != [0x16, 0x14, 0x17]:
        sys.exit(f"✗ не FakeTLS-ответ прокси (записи {[hex(r[0]) for r in recs]}) — секрет не принят, отдана маска")
    raw = b"".join(bytes([t]) + b"\x03\x03" + struct.pack(">H", len(p)) + p for t, p in recs)
    srv_random = recs[0][1][6:38]
    zeroed = raw[:11] + b"\x00" * 32 + raw[43:]
    want = hmac.new(secret, rnd + zeroed, hashlib.sha256).digest()
    if srv_random != want:
        # так отвечает маска: секрет не принят, и прокси отдал соединение настоящему сайту
        sys.exit("✗ секрет не принят: ответил настоящий сайт-маска (подпись TLS-ответа не от секрета)")

    # 3. obfuscated2: 64 байта, тег 0xdddddddd (padded intermediate), номер DC
    while True:
        init = bytearray(os.urandom(64))
        if init[0] != 0xEF and init[4:8] != b"\x00" * 4 and bytes(init[:4]) not in (
                b"HEAD", b"POST", b"GET ", b"OPTI", b"\xdd" * 4, b"\xee" * 4, b"\x16\x03\x01\x02"):
            break
    init[56:60] = b"\xdd" * 4
    init[60:62] = struct.pack("<h", dc)
    enc = ctr(hashlib.sha256(bytes(init[8:40]) + secret).digest(), bytes(init[40:56]))
    rev = bytes(init[8:56])[::-1]
    dec = ctr(hashlib.sha256(rev[:32] + secret).digest(), rev[32:48])
    e = enc.update(bytes(init))
    first = bytes(init[:56]) + e[56:64]

    # 4. req_pq_multi без ключа (auth_key_id = 0)
    nonce = os.urandom(16)
    msg_id = (int(time.time() * 2 ** 32) // 4) * 4
    body = struct.pack("<I", 0xBE7E8EF1) + nonce
    msg = struct.pack("<qqi", 0, msg_id, len(body)) + body
    frame = struct.pack("<I", len(msg)) + msg
    payload = first + enc.update(frame)
    s.sendall(b"\x14\x03\x03\x00\x01\x01" + b"\x17\x03\x03" + struct.pack(">H", len(payload)) + payload)

    # 5. ответ: TLS-записи → поток obfuscated2 → кадр → resPQ (0x05162463) с нашим nonce
    buf = b""
    while len(buf) < 4 + 20 + 4 + 16:
        t, p = recv_record(s)
        if t == 0x17:
            buf += dec.update(p)
    n = struct.unpack("<I", buf[:4])[0] & 0x7FFFFFFF
    while len(buf) < 4 + n:
        t, p = recv_record(s)
        if t == 0x17:
            buf += dec.update(p)
    m = buf[4:4 + n]
    ctor = struct.unpack("<I", m[20:24])[0]
    ms = int((time.time() - t0) * 1000)
    if ctor == 0x05162463 and m[24:40] == nonce:
        print(f"✓ MTProto через прокси {host}:{port} (маска {domain}): Telegram DC {dc} ответил resPQ, {ms} мс")
        return
    sys.exit(f"✗ неожиданный ответ: конструктор {ctor:#x}")


if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        sys.exit(f"✗ {type(e).__name__}: {e}")
