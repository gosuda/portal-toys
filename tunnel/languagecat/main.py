#!/usr/bin/env python3
"""Languagecat backend server.

This module is a minimal HTTP server that keeps track of a cat-themed
programming language leaderboard. The frontend is a static page under
python/languagecat/static/index.html, which expects two JSON endpoints:

    GET  /api/state -> current scoreboard
    POST /api/click -> apply batched vote deltas

Persistent scores are written to python/languagecat/data/scores.json.
"""

from __future__ import annotations

import argparse
import json
import os
import threading
from datetime import datetime, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Dict, Iterable, List
from urllib.parse import urlparse


ROOT = Path(__file__).resolve().parent
STATIC_DIR = ROOT / "static"
DATA_DIR = ROOT / "data"
STATE_FILE = DATA_DIR / "scores.json"
MAX_BODY = 256 * 1024
MAX_DELTA = 1_000

# The UI renders languages in the order provided here.
LANGUAGES: List[Dict[str, str]] = [
    {
        "id": "python",
        "name": "Python",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/python/python-original.svg",
    },
    {
        "id": "go",
        "name": "Go",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/go/go-original.svg",
    },
    {
        "id": "javascript",
        "name": "JavaScript",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/javascript/javascript-original.svg",
    },
    {
        "id": "rust",
        "name": "Rust",
        "icon": "/rust.png",
    },
    {
        "id": "java",
        "name": "Java",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/java/java-original.svg",
    },
    {
        "id": "csharp",
        "name": "C#",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/csharp/csharp-original.svg",
    },
    {
        "id": "ruby",
        "name": "Ruby",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/ruby/ruby-original.svg",
    },
    {
        "id": "swift",
        "name": "Swift",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/swift/swift-original.svg",
    },
    {
        "id": "cpp",
        "name": "C++",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/cplusplus/cplusplus-original.svg",
    },
    {
        "id": "kotlin",
        "name": "Kotlin",
        "icon": "https://cdn.jsdelivr.net/gh/devicons/devicon/icons/kotlin/kotlin-original.svg",
    },
]
LANGUAGE_META = {item["id"]: item for item in LANGUAGES}


class LeaderboardStore:
    """Thread-safe score persistence with a JSON file backend."""

    def __init__(self, state_file: Path, languages: Iterable[str]):
        self._file = state_file
        self._scores: Dict[str, int] = {lang_id: 0 for lang_id in languages}
        self._lock = threading.Lock()
        self._total = 0
        DATA_DIR.mkdir(parents=True, exist_ok=True)
        self._load()

    def _load(self) -> None:
        if not self._file.exists():
            return
        try:
            data = json.loads(self._file.read_text(encoding="utf-8"))
        except Exception:
            return
        scores = data.get("scores")
        if isinstance(scores, dict):
            for lang_id, value in scores.items():
                if lang_id in self._scores and isinstance(value, (int, float)):
                    self._scores[lang_id] = max(0, int(value))
        self._total = sum(self._scores.values())

    def _persist(self) -> None:
        payload = {
            "scores": self._scores,
            "updatedAt": datetime.utcnow().replace(tzinfo=timezone.utc).isoformat(),
        }
        tmp = self._file.with_suffix(".tmp")
        tmp.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
        tmp.replace(self._file)

    def state(self) -> Dict[str, Any]:
        with self._lock:
            languages = []
            for meta in LANGUAGES:
                lang_id = meta["id"]
                languages.append({**meta, "score": self._scores[lang_id]})
            return {"languages": languages, "total": self._total}

    def apply_batch(self, batch: Iterable[Dict[str, Any]]) -> Dict[str, Any]:
        updates: List[Dict[str, Any]] = []
        delta_total = 0
        with self._lock:
            for item in batch:
                lang_id = item.get("languageId")
                if lang_id not in self._scores:
                    continue
                delta = item.get("delta")
                if not isinstance(delta, (int, float)):
                    continue
                delta = int(delta)
                if not delta:
                    continue
                delta = max(-MAX_DELTA, min(MAX_DELTA, delta))
                prev = self._scores[lang_id]
                new_score = prev + delta
                if new_score == prev:
                    continue
                self._scores[lang_id] = new_score
                delta_total += new_score - prev
                updates.append({"languageId": lang_id, "score": new_score})
            if updates:
                self._total = self._total + delta_total
                self._persist()
            return {"updates": updates, "total": self._total}


STORE = LeaderboardStore(STATE_FILE, LANGUAGE_META.keys())


class Handler(BaseHTTPRequestHandler):
    server_version = "Languagecat/1.0"

    def do_GET(self) -> None:  # noqa: N802 (BaseHTTPRequestHandler API)
        parsed = urlparse(self.path)
        route = parsed.path
        if route in ("/", "/index.html"):
            return self._serve_static("index.html", "text/html; charset=utf-8")
        if route == "/style.css":
            return self._serve_static("style.css", "text/css; charset=utf-8")
        if route == "/rust.png":
            return self._serve_static("rust.png", "image/png")
        if route == "/api/state":
            return self._handle_state()
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_POST(self) -> None:  # noqa: N802
        parsed = urlparse(self.path)
        if parsed.path == "/api/click":
            return self._handle_click()
        self.send_error(HTTPStatus.NOT_FOUND)

    def log_message(self, fmt: str, *args: Any) -> None:
        # Reduce noise for the toy project.
        return

    def _handle_state(self) -> None:
        payload = STORE.state()
        self._write_json(HTTPStatus.OK, payload)

    def _handle_click(self) -> None:
        length = int(self.headers.get("Content-Length") or 0)
        if length <= 0 or length > MAX_BODY:
            return self._write_json(
                HTTPStatus.BAD_REQUEST,
                {"error": "invalid body size"},
            )
        try:
            body = self.rfile.read(length)
        except Exception:
            return self._write_json(HTTPStatus.BAD_REQUEST, {"error": "failed to read body"})
        try:
            batch = json.loads(body.decode("utf-8"))
        except Exception:
            return self._write_json(HTTPStatus.BAD_REQUEST, {"error": "invalid json"})
        if not isinstance(batch, list):
            return self._write_json(HTTPStatus.BAD_REQUEST, {"error": "expected list payload"})
        result = STORE.apply_batch(batch)
        self._write_json(HTTPStatus.OK, result)

    def _serve_static(self, filename: str, content_type: str) -> None:
        path = STATIC_DIR / filename
        if not path.exists():
            self.send_error(HTTPStatus.NOT_FOUND)
            return
        data = path.read_bytes()
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _write_json(self, status: int, payload: Dict[str, Any]) -> None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Languagecat backend (Python)")
    parser.add_argument("--host", default="127.0.0.1", help="Bind host (default: 127.0.0.1)")
    parser.add_argument("--port", type=int, default=8083, help="Bind port (default: 8083)")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    httpd = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"Languagecat running on http://{args.host}:{args.port}")
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        httpd.server_close()


if __name__ == "__main__":
    main()
