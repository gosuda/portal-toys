#!/usr/bin/env python3
import argparse
import contextlib
import dataclasses
import ipaddress
import json
import os
import re
import socket
import threading
import time
import urllib.parse
import sys
from http import HTTPStatus
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from subprocess import Popen, PIPE

# Prefer Python module import over external binary
try:
    import yt_dlp as ytdlp  # type: ignore
    YTDLP_AVAILABLE = True
except Exception:
    ytdlp = None  # type: ignore
    YTDLP_AVAILABLE = False


# When frozen (PyInstaller), static files live under sys._MEIPASS, but
# writable data should live next to the executable (sys.executable).
IS_FROZEN = getattr(sys, 'frozen', False)
MEIPASS = getattr(sys, '_MEIPASS', None)
APP_DIR = Path(sys.executable).resolve().parent if IS_FROZEN else Path(__file__).resolve().parent
ROOT = APP_DIR
STATIC = (Path(MEIPASS) / "static") if (IS_FROZEN and MEIPASS) else (ROOT / "static")
DOWNLOADS = APP_DIR / "data"
DOWNLOADS.mkdir(parents=True, exist_ok=True)


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


@dataclasses.dataclass
class Job:
    id: str
    url: str
    status: str = "queued"  # queued|downloading|done|error
    progress: float = 0.0
    log: list[str] = dataclasses.field(default_factory=list)
    files: list[str] = dataclasses.field(default_factory=list)
    error: str | None = None


class Jobs:
    def __init__(self, base: Path):
        self.base = base
        self._lock = threading.Lock()
        self._jobs: dict[str, Job] = {}

    def create(self, url: str) -> Job:
        jid = f"j{int(time.time()*1000):x}"
        job = Job(id=jid, url=url)
        with self._lock:
            self._jobs[jid] = job
        return job

    def get(self, jid: str) -> Job | None:
        with self._lock:
            return self._jobs.get(jid)

    def list(self) -> list[Job]:
        with self._lock:
            return list(self._jobs.values())


JOBS = Jobs(DOWNLOADS)
LOG_LOCK = threading.Lock()


def run_download(job: Job):
    job_dir = DOWNLOADS / job.id
    job_dir.mkdir(parents=True, exist_ok=True)
    args = [
        resolve_yt_dlp(),
        "--no-playlist",
        "--newline",
        "-P", str(job_dir),
        "-o", "%(title)s.%(ext)s",
        job.url,
    ]
    job.status = "downloading"
    try:
        with Popen(args, stdout=PIPE, stderr=PIPE, text=True, bufsize=1) as proc:
            def append(line: str):
                line = line.rstrip()
                if not line:
                    return
                with LOG_LOCK:
                    job.log.append(line)
                    if len(job.log) > 200:
                        job.log = job.log[-200:]

            def read_stdout():
                if not proc.stdout:
                    return
                for line in proc.stdout:
                    append(line)
                    m = re.search(r"(\d+(?:\.\d+)?)%", line)
                    if m:
                        with LOG_LOCK:
                            try:
                                job.progress = float(m.group(1))
                            except Exception:
                                pass

            def read_stderr():
                if not proc.stderr:
                    return
                for line in proc.stderr:
                    append(line)

            t_out = threading.Thread(target=read_stdout, daemon=True)
            t_err = threading.Thread(target=read_stderr, daemon=True)
            t_out.start(); t_err.start()
            rc = proc.wait()
            t_out.join(timeout=0.2); t_err.join(timeout=0.2)
            if rc != 0:
                with LOG_LOCK:
                    job.status = "error"
                    job.error = "\n".join(job.log[-10:])[-4000:] or "yt-dlp failed"
                return
    except FileNotFoundError:
        job.status = "error"
        job.error = "yt-dlp not found. Install with: pip install yt-dlp"
        return
    except Exception as e:
        job.status = "error"
        job.error = f"{type(e).__name__}: {e}"[:4000]
        return

    # Scan files
    files = []
    for p in sorted(job_dir.rglob("*")):
        if p.is_file():
            try:
                rel = p.relative_to(DOWNLOADS).as_posix()
            except Exception:
                rel = p.as_posix()
            files.append(rel)
    job.files = files
    job.progress = 100.0
    job.status = "done"


def run_download_ytdlp(job: Job):
    """Downloader using the yt_dlp Python module (preferred)."""
    job_dir = DOWNLOADS / job.id
    job_dir.mkdir(parents=True, exist_ok=True)
    with LOG_LOCK:
        job.status = "downloading"

    if not YTDLP_AVAILABLE:
        with LOG_LOCK:
            job.status = "error"
            job.error = "yt_dlp module not installed. Run: pip install -r python/yt-dlp/requirements.txt"
        return

    class _Logger:
        def _append(self, level: str, msg: str):
            line = f"[{level}] {msg}".rstrip()
            with LOG_LOCK:
                job.log.append(line)
                if len(job.log) > 200:
                    job.log = job.log[-200:]

        def debug(self, msg):
            self._append("D", str(msg))

        def info(self, msg):
            self._append("I", str(msg))

        def warning(self, msg):
            self._append("W", str(msg))

        def error(self, msg):
            self._append("E", str(msg))

    def hook(d: dict):
        try:
            if d.get('status') == 'downloading':
                total = d.get('total_bytes') or d.get('total_bytes_estimate') or 0
                downloaded = d.get('downloaded_bytes') or 0
                if total:
                    pct = max(0.0, min(100.0, (downloaded / total) * 100.0))
                    with LOG_LOCK:
                        job.progress = pct
            elif d.get('status') == 'finished':
                with LOG_LOCK:
                    job.progress = 100.0
        except Exception:
            pass

    ydl_opts = {
        'noplaylist': True,
        'outtmpl': str((job_dir / '%(title)s.%(ext)s').as_posix()),
        'logger': _Logger(),
        'progress_hooks': [hook],
        'quiet': True,
        'no_warnings': True,
    }

    try:
        with ytdlp.YoutubeDL(ydl_opts) as ydl:  # type: ignore[attr-defined]
            ydl.download([job.url])
    except Exception as e:
        with LOG_LOCK:
            job.status = "error"
            job.error = f"{type(e).__name__}: {e}"[:4000]
        return

    # After finish, scan files
    files = []
    for p in sorted(job_dir.rglob("*")):
        if p.is_file():
            try:
                rel = p.relative_to(DOWNLOADS).as_posix()
            except Exception:
                rel = p.as_posix()
            files.append(rel)
    with LOG_LOCK:
        job.files = files
        job.progress = 100.0
        job.status = "done"


def resolve_yt_dlp() -> str:
    exe = "yt-dlp.exe" if os.name == "nt" else "yt-dlp"
    # Try PATH first
    return os.getenv("YTDLP_BIN", exe)


def parse_form(body: bytes) -> dict:
    try:
        data = urllib.parse.parse_qs(body.decode("utf-8"), keep_blank_values=True)
        return {k: v[0] if isinstance(v, list) and v else "" for k, v in data.items()}
    except Exception:
        return {}


class Handler(BaseHTTPRequestHandler):
    server_version = "YtDlpHostPy/0.1"

    def do_GET(self):  # noqa: N802
        if self.path == "/" or self.path.startswith("/index.html"):
            return self._serve_file(STATIC / "index.html", "text/html; charset=utf-8")
        if self.path.startswith("/style.css"):
            return self._serve_file(STATIC / "style.css", "text/css; charset=utf-8")
        if self.path.startswith("/api/status"):
            return self._status()
        if self.path.startswith("/api/jobs"):
            return self._jobs()
        if self.path.startswith("/files/"):
            return self._serve_download()
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_POST(self):  # noqa: N802
        if self.path.startswith("/api/download"):
            return self._download()
        self.send_error(HTTPStatus.NOT_FOUND)

    def _download(self):
        length = int(self.headers.get("Content-Length", "0") or 0)
        body = self.rfile.read(min(length, 1_000_000))
        form = parse_form(body)
        url = (form.get("url") or "").strip()
        if not url:
            return self._json(HTTPStatus.BAD_REQUEST, {"error": "missing url"})
        try:
            u = urllib.parse.urlparse(url)
        except Exception:
            return self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid url"})
        if u.scheme not in ("http", "https") or not u.netloc:
            return self._json(HTTPStatus.BAD_REQUEST, {"error": "invalid url"})
        if not host_is_public(u.hostname or ""):
            return self._json(HTTPStatus.FORBIDDEN, {"error": "forbidden host"})

        if not YTDLP_AVAILABLE:
            return self._json(HTTPStatus.INTERNAL_SERVER_ERROR, {"error": "yt_dlp module not installed. Run: pip install -r python/yt-dlp/requirements.txt"})

        job = JOBS.create(url)
        # Module-only downloader
        t = threading.Thread(target=run_download_ytdlp, args=(job,), daemon=True)
        t.start()
        self._json(HTTPStatus.ACCEPTED, {"id": job.id, "status": job.status})

    def _status(self):
        q = urllib.parse.urlparse(self.path).query
        jid = urllib.parse.parse_qs(q).get("id", [""])[0]
        job = JOBS.get(jid)
        if not job:
            return self._json(HTTPStatus.NOT_FOUND, {"error": "job not found"})
        self._json(HTTPStatus.OK, dataclasses.asdict(job))

    def _jobs(self):
        js = [dataclasses.asdict(j) for j in JOBS.list()]
        self._json(HTTPStatus.OK, js)

    def _serve_download(self):
        # Path: /files/<job_id>/<file>
        parts = self.path.split("/", 3)
        if len(parts) < 4:
            return self.send_error(HTTPStatus.NOT_FOUND)
        sub = parts[3]
        # prevent path traversal
        p = (DOWNLOADS / sub).resolve()
        if not str(p).startswith(str(DOWNLOADS.resolve())):
            return self.send_error(HTTPStatus.FORBIDDEN)
        if not p.exists() or not p.is_file():
            return self.send_error(HTTPStatus.NOT_FOUND)
        # naive content-type
        ctype = "application/octet-stream"
        if p.suffix in {".mp4", ".m4v", ".mov", ".webm"}:
            ctype = "video/mp4" if p.suffix in {".mp4", ".m4v"} else "video/webm"
        elif p.suffix in {".mp3", ".m4a", ".aac", ".opus", ".ogg", ".wav", ".flac"}:
            ctype = "audio/mpeg"
        self._serve_file(p, ctype)

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

    def _json(self, status: int, obj: dict):
        data = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
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
    parser = argparse.ArgumentParser(description="Portal demo: yt-dlp URL downloader host (Python)")
    parser.add_argument("--port", type=int, default=8084, help="local HTTP port")
    parser.add_argument("--host", type=str, default="127.0.0.1", help="bind host")
    parser.add_argument("--name", type=str, default="yt-dlp", help="display name for relay UI")
    parser.add_argument("--server-url", type=str, default=os.getenv("RELAY") or os.getenv("RELAY_URL") or "wss://portal.gosuda.org/relay", help="relay websocket URL")
    args = parser.parse_args()

    httpd = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"yt-dlp host running on http://{args.host}:{args.port}")
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


if __name__ == "__main__":
    main()
