import os
import argparse
import contextlib
from subprocess import Popen
from datetime import datetime
from pathlib import Path
from typing import List

from flask import (
    Flask,
    abort,
    flash,
    redirect,
    render_template,
    request,
    send_from_directory,
    url_for,
)
from werkzeug.utils import secure_filename
import re
import unicodedata


ALLOWED_EXTENSIONS = {"png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff"}


def get_image_dir() -> Path:
    # IMAGE_DIR can be absolute or relative. Defaults to ./python/image_server/images
    base = os.environ.get("IMAGE_DIR")
    if base:
        p = Path(base)
    else:
        p = Path(__file__).resolve().parent / "data"
    p.mkdir(parents=True, exist_ok=True)
    return p


def allowed_file(filename: str) -> bool:
    return "." in filename and filename.rsplit(".", 1)[1].lower() in ALLOWED_EXTENSIONS


def list_images(img_dir: Path) -> List[dict]:
    items: List[dict] = []
    for entry in sorted(img_dir.iterdir()):
        if entry.is_file() and allowed_file(entry.name):
            stat = entry.stat()
            items.append(
                {
                    "name": entry.name,
                    "size": stat.st_size,
                    "mtime": datetime.fromtimestamp(stat.st_mtime),
                }
            )
    return items


def create_app() -> Flask:
    app = Flask(__name__)
    app.config["SECRET_KEY"] = os.environ.get("SECRET_KEY", "dev-secret-key")
    app.config["IMAGE_DIR"] = str(get_image_dir())

    @app.route("/")
    def index():
        images = list_images(Path(app.config["IMAGE_DIR"]))
        return render_template("index.html", images=images)

    def _sanitize_filename(name: str) -> str:
        # Keep Unicode characters; strip directory parts and illegal characters (Windows-safe set)
        base = os.path.basename(name)
        base = unicodedata.normalize("NFC", base)
        # Remove control chars and reserved characters <>:"/\|?*
        base = re.sub(r'[<>:"/\\|?*\x00-\x1F]', "", base)
        # Collapse whitespace
        base = re.sub(r"\s+", " ", base).strip()
        # Avoid leading/trailing dots or spaces (Windows)
        base = base.strip(" .")
        # Prevent empty names
        return base or ""

    def _unique_path(dir_path: Path, filename: str) -> Path:
        base, ext = os.path.splitext(filename)
        candidate = dir_path / (base + ext)
        i = 1
        while candidate.exists():
            candidate = dir_path / f"{base}-{i}{ext}"
            i += 1
        return candidate

    @app.route("/upload", methods=["POST"])
    def upload():
        if "file" not in request.files:
            flash("파일 파트가 없습니다.")
            return redirect(url_for("index"))
        file = request.files["file"]
        if file.filename == "":
            flash("선택된 파일이 없습니다.")
            return redirect(url_for("index"))
        if file and allowed_file(file.filename):
            # Preserve Unicode while sanitizing dangerous characters
            original = file.filename
            sanitized = _sanitize_filename(original)
            # If sanitization removed everything, synthesize a name
            if not sanitized:
                ext = original.rsplit(".", 1)[1].lower() if "." in original else "img"
                sanitized = f"upload-{datetime.utcnow().strftime('%Y%m%d-%H%M%S%f')[:17]}.{ext}"
            # Ensure extension exists and is allowed
            if "." not in sanitized:
                ext = original.rsplit(".", 1)[1].lower() if "." in original else ""
                if ext:
                    sanitized = f"{sanitized}.{ext}"
            # Enforce a reasonable max length
            if len(sanitized) > 200:
                root, dot, ext = sanitized.rpartition(".")
                root = root[: max(1, 200 - (len(ext) + (1 if dot else 0)))]
                sanitized = f"{root}{dot}{ext}" if dot else root
            img_dir = Path(app.config["IMAGE_DIR"]) 
            save_path = _unique_path(img_dir, sanitized)
            file.save(str(save_path))
            flash("업로드 완료")
            return redirect(url_for("index"))
        flash("허용되지 않는 확장자입니다.")
        return redirect(url_for("index"))

    @app.route("/images/<path:filename>")
    def serve_image(filename: str):
        # Prevent path traversal and restrict to allowed extensions
        if not allowed_file(filename):
            abort(404)
        return send_from_directory(app.config["IMAGE_DIR"], filename)

    @app.route("/download/<path:filename>")
    def download_image(filename: str):
        if not allowed_file(filename):
            abort(404)
        return send_from_directory(app.config["IMAGE_DIR"], filename, as_attachment=True)

    return app


def maybe_start_tunnel(port: int, host: str, name: str, relays: list[str] | None) -> Popen | None:
    enabled = (os.getenv("TUNNEL_ENABLED", "true").lower() in ("1", "true", "yes", "on"))
    if not enabled:
        return None
    bin_path = os.getenv("TUNNEL_BIN") or os.getenv("PORTAL_TUNNEL_BIN") or (
        Path.cwd().parent.parent / "bin" / ("portal-tunnel.exe" if os.name == "nt" else "portal-tunnel")
    ).as_posix()
    args = [bin_path, "expose", "--port", str(port), "--host", host]
    if relays:
        relay_arg = ",".join(relays)
        args += ["--relay", relay_arg]
    if name:
        args += ["--name", name]
    try:
        return Popen(args)
    except FileNotFoundError:
        print("portal-tunnel not found. Install with: make tunnel-install")
        return None


def _split_relays(val: str | None) -> list[str]:
    if not val:
        return []
    out: list[str] = []
    for part in str(val).split(","):
        s = part.strip()
        if s:
            out.append(s)
    # de-duplicate preserving order
    seen = set()
    uniq: list[str] = []
    for u in out:
        if u not in seen:
            uniq.append(u)
            seen.add(u)
    return uniq


def main():
    parser = argparse.ArgumentParser(description="Portal demo: Image Server (Python)")
    parser.add_argument("--port", type=int, default=int(os.getenv("PORT", "8084")), help="local HTTP port")
    parser.add_argument("--host", type=str, default=os.getenv("HOST", "127.0.0.1"), help="bind host")
    parser.add_argument("--name", type=str, default=os.getenv("NAME", "py-image-server"), help="display name for relay UI")
    parser.add_argument(
        "--server-url",
        dest="server_url",
        action="append",
        help="relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)",
    )
    parser.add_argument("--image-dir", type=str, default=os.getenv("IMAGE_DIR", ""), help="host path for storing images")
    parser.add_argument("--debug", action="store_true", help="enable Flask debug mode")
    args = parser.parse_args()

    # Prefer CLI over env for IMAGE_DIR
    if args.image_dir:
        os.environ["IMAGE_DIR"] = args.image_dir

    app = create_app()
    print(f"Image Server running on http://{args.host}:{args.port}")

    # Optionally start tunnel process
    relays: list[str] = []
    # Collect from CLI (repeatable) and allow comma-separated in each occurrence
    if args.server_url:
        for v in args.server_url:
            relays.extend(_split_relays(v))
    # Fallback to env if none provided via CLI
    if not relays:
        relays = _split_relays(os.getenv("RELAY") or os.getenv("RELAY_URL"))
    proc = maybe_start_tunnel(args.port, args.host, args.name, relays if relays else None)
    try:
        app.run(host=args.host, port=args.port, debug=bool(args.debug))
    finally:
        if proc and proc.poll() is None:
            with contextlib.suppress(Exception):
                proc.terminate()


if __name__ == "__main__":
    main()
