#!/usr/bin/env python3
import argparse
import contextlib
import dataclasses
import datetime as dt
import io
import ipaddress
import json
import os
import random
import socket
import threading
import time
import urllib.parse
import urllib.request
from http import HTTPStatus
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from subprocess import Popen


DATA_DIR = Path(__file__).resolve().parent / "data"
STATIC_DIR = Path(__file__).resolve().parent / "static"

DELETE_THRESHOLD = 3
MAX_LEN = 2500


@dataclasses.dataclass
class Message:
    id: str
    nickname: str
    content: str
    timestamp: str  # ISO8601


class Store:
    def __init__(self, base: Path):
        self.base = base
        self.base.mkdir(parents=True, exist_ok=True)
        self.msg_file = self.base / "messages.jsonl"
        self.votes_file = self.base / "votes.json"
        self._lock = threading.Lock()
        self._messages = []  # newest first
        self._votes = {}     # id -> count
        self._load()

    def _load(self):
        with self._lock:
            self._messages.clear()
            if self.msg_file.exists():
                with self.msg_file.open("r", encoding="utf-8") as f:
                    for line in f:
                        line = line.strip()
                        if not line:
                            continue
                        try:
                            obj = json.loads(line)
                            self._messages.append(Message(**obj))
                        except Exception:
                            continue
            # newest first (file is append-only)
            self._messages.reverse()
            if self.votes_file.exists():
                try:
                    self._votes = json.loads(self.votes_file.read_text(encoding="utf-8"))
                except Exception:
                    self._votes = {}

    def list(self):
        with self._lock:
            out = []
            for m in self._messages:
                out.append({
                    "id": m.id,
                    "nickname": m.nickname,
                    "content": m.content,
                    "timestamp": m.timestamp,
                    "voteCount": int(self._votes.get(m.id, 0)),
                })
            return out

    def _persist_vote(self):
        tmp = self.votes_file.with_suffix(".tmp")
        tmp.write_text(json.dumps(self._votes, ensure_ascii=False), encoding="utf-8")
        tmp.replace(self.votes_file)

    def submit(self, nickname: str, content: str):
        ts = dt.datetime.utcnow().replace(tzinfo=dt.timezone.utc).isoformat()
        mid = make_msg_id()
        msg = Message(id=mid, nickname=nickname, content=content, timestamp=ts)
        with self._lock:
            # append to file
            with self.msg_file.open("a", encoding="utf-8") as f:
                f.write(json.dumps(dataclasses.asdict(msg), ensure_ascii=False) + "\n")
            # newest first
            self._messages.insert(0, msg)
        return mid

    def vote_delete(self, mid: str, threshold: int):
        with self._lock:
            n = int(self._votes.get(mid, 0)) + 1
            self._votes[mid] = n
            self._persist_vote()
            if n >= max(1, threshold):
                # delete message in-memory and rewrite file
                self._messages = [m for m in self._messages if m.id != mid]
                # rewrite file newest-last
                with self.msg_file.open("w", encoding="utf-8") as f:
                    for m in reversed(self._messages):
                        f.write(json.dumps(dataclasses.asdict(m), ensure_ascii=False) + "\n")
                self._votes.pop(mid, None)
                self._persist_vote()
                return True, n
            return False, n


def make_msg_id() -> str:
    # Similar format to Go: "m:<rev_ts_hex>:<rand_hex>"
    rev_ts = (~int(time.time_ns())) & ((1 << 64) - 1)
    rnd = random.getrandbits(32)
    return f"m:{rev_ts:016x}:{rnd:08x}"


def parse_form(body: bytes) -> dict:
    try:
        data = urllib.parse.parse_qs(body.decode("utf-8"), keep_blank_values=True)
        return {k: v[0] if isinstance(v, list) and v else "" for k, v in data.items()}
    except Exception:
        return {}


def host_is_public(host: str) -> bool:
    try:
        for info in socket.getaddrinfo(host, None):
            addr = info[4][0]
            ip = ipaddress.ip_address(addr)
            if ip.is_loopback or ip.is_private or ip.is_link_local or ip.is_multicast or ip.is_unspecified:
                return False
        return True
    except Exception:
        return False


class Handler(BaseHTTPRequestHandler):
    server_version = "RollingPaperPy/0.1"

    def do_GET(self):  # noqa: N802
        if self.path.startswith("/api/messages"):
            return self._get_messages()
        if self.path.startswith("/api/proxy"):
            return self._proxy()
        # Static files
        if self.path in ("/", "/index.html"):
            return self._serve_file(STATIC_DIR / "index.html", "text/html; charset=utf-8")
        if self.path.startswith("/style.css"):
            return self._serve_file(STATIC_DIR / "style.css", "text/css; charset=utf-8")
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_POST(self):  # noqa: N802
        if self.path.startswith("/api/submit"):
            return self._submit()
        if self.path.startswith("/api/vote-delete"):
            return self._vote_delete()
        self.send_error(HTTPStatus.NOT_FOUND)

    def _get_messages(self):
        payload = {
            "messages": STORE.list(),
            "threshold": DELETE_THRESHOLD,
            "maxLen": MAX_LEN,
        }
        self._write_json(HTTPStatus.OK, payload)

    def _submit(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(min(length, 1_000_000))
        form = parse_form(body)
        content = (form.get("message") or "").strip()
        nickname = (form.get("nickname") or "").strip()
        if not content:
            return self._write_json(HTTPStatus.BAD_REQUEST, {"status": "error", "message": "message required"})
        if len(content) > MAX_LEN:
            return self._write_json(HTTPStatus.BAD_REQUEST, {"status": "error", "message": f"Too long message (maximum {MAX_LEN} characters)"})
        STORE.submit(nickname=nickname, content=content)
        self._write_json(HTTPStatus.OK, {"status": "success", "message": "ok"})

    def _vote_delete(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(min(length, 64 * 1024))
        form = parse_form(body)
        mid = (form.get("id") or "").strip()
        if not mid:
            return self._write_json(HTTPStatus.BAD_REQUEST, {"status": "error", "message": "missing id"})
        deleted, count = STORE.vote_delete(mid, DELETE_THRESHOLD)
        self._write_json(HTTPStatus.OK, {
            "status": "success",
            "deleted": deleted,
            "voteCount": count,
            "threshold": DELETE_THRESHOLD,
        })

    def _proxy(self):
        try:
            q = urllib.parse.urlparse(self.path).query
            u = urllib.parse.parse_qs(q).get("u", [""])[0]
        except Exception:
            u = ""
        if not u:
            return self.send_error(HTTPStatus.BAD_REQUEST, "missing u")
        if len(u) > 2048:
            return self.send_error(HTTPStatus.BAD_REQUEST, "url too long")
        try:
            p = urllib.parse.urlparse(u)
        except Exception:
            return self.send_error(HTTPStatus.BAD_REQUEST, "invalid url")
        if p.scheme not in ("http", "https") or not p.netloc:
            return self.send_error(HTTPStatus.BAD_REQUEST, "invalid url")
        if not host_is_public(p.hostname or ""):
            return self.send_error(HTTPStatus.FORBIDDEN, "forbidden host")
        # Fetch with timeout and basic header passthrough
        req = urllib.request.Request(u, headers={"User-Agent": "rolling-paper-proxy/0.1", "Referer": ""})
        try:
            with contextlib.closing(urllib.request.urlopen(req, timeout=12)) as resp:
                status = getattr(resp, "status", 200)
                if status < 200 or status >= 400:
                    return self.send_error(HTTPStatus.BAD_GATEWAY, "upstream status")
                ct = resp.headers.get("Content-Type", "application/octet-stream")
                self.send_response(HTTPStatus.OK)
                self.send_header("Content-Type", ct)
                self.send_header("Cache-Control", "public, max-age=300")
                self.end_headers()
                # Limit copy to 10MB
                remaining = 10 * 1024 * 1024
                while remaining > 0:
                    chunk = resp.read(min(65536, remaining))
                    if not chunk:
                        break
                    self.wfile.write(chunk)
                    remaining -= len(chunk)
        except Exception:
            return self.send_error(HTTPStatus.BAD_GATEWAY, "upstream error")

    def _serve_file(self, path: Path, ctype: str):
        if not path.exists():
            return self.send_error(HTTPStatus.NOT_FOUND)
        try:
            data = path.read_bytes()
            self.send_response(HTTPStatus.OK)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception:
            self.send_error(HTTPStatus.INTERNAL_SERVER_ERROR)

    def _write_json(self, status: int, obj: dict):
        data = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        self.send_header("Pragma", "no-cache")
        self.send_header("Expires", "0")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def maybe_start_tunnel(port: int, host: str, name: str, relay: str | None) -> Popen | None:
    enabled = (os.getenv("TUNNEL_ENABLED", "true").lower() in ("1", "true", "yes", "on"))
    if not enabled:
        return None
    bin_path = os.getenv("TUNNEL_BIN") or os.getenv("PORTAL_TUNNEL_BIN") or (Path.cwd().parent.parent / "bin" / ("portal-tunnel.exe" if os.name == "nt" else "portal-tunnel")).as_posix()
    args = [bin_path, "expose", "--port", str(port), "--host", host]
    if relay:
        args += ["--relay", relay]
    if name:
        args += ["--name", name]
    try:
        return Popen(args)
    except FileNotFoundError:
        print("portal-tunnel not found. Install with: make tunnel-install")
        return None


def main():
    parser = argparse.ArgumentParser(description="Portal demo: Rolling Paper (Python)")
    parser.add_argument("--port", type=int, default=8083, help="local HTTP port")
    parser.add_argument("--host", type=str, default="127.0.0.1", help="bind host")
    parser.add_argument("--name", type=str, default="py-rolling-paper", help="display name for relay UI")
    parser.add_argument("--server-url", type=str, default=os.getenv("RELAY") or os.getenv("RELAY_URL"), help="relay websocket URL")
    args = parser.parse_args()

    DATA_DIR.mkdir(parents=True, exist_ok=True)
    httpd = ThreadingHTTPServer((args.host, args.port), Handler)

    print(f"Rolling Paper (Python) running on http://{args.host}:{args.port}")
    # Optionally start tunnel process
    proc = maybe_start_tunnel(args.port, args.host, args.name, args.server_url)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        httpd.server_close()
        if proc and proc.poll() is None:
            with contextlib.suppress(Exception):
                proc.terminate()


STORE = Store(DATA_DIR)

if __name__ == "__main__":
    main()
