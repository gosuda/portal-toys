#!/usr/bin/env python3
import argparse
import contextlib
import json
import os
import sys
import threading
from http import HTTPStatus
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from subprocess import Popen
from typing import Any, Dict, List

# LiteLLM supports many providers (ollama, openai-compatible servers, vLLM, llama.cpp, etc.)
try:
    from litellm import completion  # type: ignore
    import litellm  # type: ignore
except Exception:
    completion = None  # type: ignore
    litellm = None  # type: ignore


APP_DIR = Path(__file__).resolve().parent
STATIC = APP_DIR / "static"


def read_body_json(handler: BaseHTTPRequestHandler) -> Dict[str, Any]:
    try:
        length = int(handler.headers.get("Content-Length", "0") or 0)
        raw = handler.rfile.read(min(length, 5_000_000))  # 5 MB cap
        return json.loads(raw or b"{}")
    except Exception:
        return {}


def parse_providers_json(raw: str | None) -> List[Dict[str, Any]]:
    if not raw:
        return []
    try:
        arr = json.loads(raw)
        if isinstance(arr, list):
            return arr
    except Exception:
        return []
    return []


def build_providers(engine: str | None, api_key: str | None, base_url: str | None, model: str | None, fallback_env: bool = True) -> List[Dict[str, Any]]:
    # Priority: explicit JSON (LLM_PROVIDERS) > CLI engine/base/model/api_key > sane defaults
    env_json = os.getenv("LLM_PROVIDERS") if fallback_env else None
    env_providers = parse_providers_json(env_json)
    if env_providers:
        return env_providers

    eng = (engine or os.getenv("LLM_ENGINE") or "ollama").strip().lower()
    api = api_key or os.getenv("LLM_API_KEY") or os.getenv("OPENAI_API_KEY") or ""
    base = base_url or os.getenv("LLM_BASE_URL") or ""
    mdl = model or os.getenv("LLM_MODEL") or ""

    prov: Dict[str, Any] = {"name": eng, "engine": eng}
    if eng == "ollama":
        prov["base_url"] = base or os.getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")
        prov["api_key"] = api or os.getenv("OLLAMA_API_KEY", "ollama")
        prov["models"] = [mdl] if mdl else ["llama3", "mistral", "phi3"]
    elif eng in ("gemini", "google"):
        # Google AI Studio (Gemini)
        prov["engine"] = "gemini"
        prov["api_key"] = api or os.getenv("GEMINI_API_KEY") or os.getenv("GOOGLE_API_KEY") or ""
        if base:
            prov["base_url"] = base
        prov["models"] = [mdl] if mdl else ["gemini-1.5-flash", "gemini-1.5-pro"]
    else:
        # Treat anything else as OpenAI-compatible
        prov["engine"] = "openai_compatible"
        prov["base_url"] = base or os.getenv("OPENAI_BASE_URL") or "http://127.0.0.1:1234/v1"
        prov["api_key"] = api or "lm-studio"
        prov["models"] = [mdl] if mdl else ["local"]
    return [prov]


PROVIDERS: List[Dict[str, Any]] = []
PROV_LOCK = threading.Lock()


def write_json(handler: BaseHTTPRequestHandler, code: int, obj: Dict[str, Any]):
    data = json.dumps(obj, ensure_ascii=False).encode("utf-8")
    handler.send_response(code)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
    handler.send_header("Content-Length", str(len(data)))
    handler.end_headers()
    handler.wfile.write(data)


def stream_header(handler: BaseHTTPRequestHandler):
    handler.send_response(HTTPStatus.OK)
    handler.send_header("Content-Type", "text/event-stream; charset=utf-8")
    handler.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
    handler.send_header("Connection", "keep-alive")
    handler.send_header("X-Accel-Buffering", "no")
    handler.end_headers()


def sse(handler: BaseHTTPRequestHandler, event: str):
    try:
        handler.wfile.write(f"data: {event}\n\n".encode("utf-8"))
        handler.wfile.flush()
    except Exception:
        pass


def call_litellm(provider: Dict[str, Any], payload: Dict[str, Any]):
    # Maps our provider spec to litellm.completion args
    engine = (provider.get("engine") or "").lower()
    api_base = provider.get("base_url") or provider.get("api_base")
    api_key = provider.get("api_key") or os.getenv("OPENAI_API_KEY", "")

    model = payload.get("model") or provider.get("model") or (provider.get("models") or [None])[0]
    messages = payload.get("messages") or []
    temperature = payload.get("temperature")
    top_p = payload.get("top_p")
    max_tokens = payload.get("max_tokens")
    stream = bool(payload.get("stream", False))

    kwargs: Dict[str, Any] = {
        "model": model,
        "messages": messages,
        "api_base": api_base,
        "api_key": api_key,
        "stream": stream,
    }
    if temperature is not None:
        kwargs["temperature"] = float(temperature)
    if top_p is not None:
        kwargs["top_p"] = float(top_p)
    if max_tokens is not None:
        kwargs["max_tokens"] = int(max_tokens)

    # Select provider
    if engine in ("ollama",):
        kwargs["custom_llm_provider"] = "ollama"
    elif engine in ("openai_compatible", "openai"):
        # e.g., LM Studio, llama.cpp server, vLLM, TGI (if OpenAI-compatible)
        kwargs["custom_llm_provider"] = "openai"
    elif engine in ("gemini", "google"):
        # Google AI Studio (Gemini)
        kwargs["custom_llm_provider"] = "gemini"
    else:
        # Fallback to openai-compatible
        kwargs["custom_llm_provider"] = "openai"

    return completion(**kwargs)  # type: ignore[misc]


class Handler(BaseHTTPRequestHandler):
    server_version = "LLMGateway/0.1"

    def do_GET(self):  # noqa: N802
        if self.path == "/" or self.path.startswith("/index.html"):
            return self._file(STATIC / "index.html", "text/html; charset=utf-8")
        if self.path.startswith("/style.css"):
            return self._file(STATIC / "style.css", "text/css; charset=utf-8")
        if self.path.startswith("/api/providers"):
            with PROV_LOCK:
                return write_json(self, HTTPStatus.OK, {"providers": PROVIDERS})
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_POST(self):  # noqa: N802
        if self.path.startswith("/api/chat"):
            return self._chat()
        self.send_error(HTTPStatus.NOT_FOUND)

    def _file(self, path: Path, ctype: str):
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

    def _chat(self):
        if completion is None:
            return write_json(self, HTTPStatus.INTERNAL_SERVER_ERROR, {"error": "litellm not installed. pip install -r python/llm-gateway/requirements.txt"})
        payload = read_body_json(self)
        name = str(payload.get("provider") or "").strip().lower()
        stream = bool(payload.get("stream") or False)
        prov = None
        with PROV_LOCK:
            for p in PROVIDERS:
                if str(p.get("name", "")).lower() == name:
                    prov = p
                    break
            if prov is None and PROVIDERS:
                prov = PROVIDERS[0]

        if prov is None:
            return write_json(self, HTTPStatus.BAD_REQUEST, {"error": "no providers configured"})

        try:
            if stream:
                stream_header(self)
                for chunk in call_litellm(prov, payload):  # generator of deltas
                    try:
                        # LiteLLM stream returns objects with .choices[0].delta or .text
                        data = chunk  # pass-through JSON is fine
                        sse(self, json.dumps(_to_openai_sse(data)))
                    except Exception:
                        # Ignore streaming chunk errors; connection may close mid-stream
                        pass
                sse(self, json.dumps({"done": True}))
                return
            else:
                resp = call_litellm(prov, payload)
                return write_json(self, HTTPStatus.OK, _to_openai_json(resp))
        except Exception as e:
            # Include basic provider context to help debugging
            ctx = {
                "engine": prov.get("engine"),
                "base_url": prov.get("base_url"),
                "model": payload.get("model") or prov.get("model") or (prov.get("models") or [None])[0],
            }
            return write_json(self, HTTPStatus.INTERNAL_SERVER_ERROR, {"error": f"{type(e).__name__}: {e}", "provider": ctx})


def _to_openai_sse(obj: Any) -> Dict[str, Any]:
    # Normalize various provider stream chunks to minimal OpenAI-like shape for the UI
    try:
        # chat format
        choices = getattr(obj, "choices", None)
        if choices:
            delta = getattr(choices[0], "delta", None) or {}
            content = getattr(delta, "content", None)
            if content is not None:
                return {"delta": str(content)}
        # text format
        text = getattr(obj, "text", None)
        if text is not None:
            return {"delta": str(text)}
    except Exception:
        pass
    # best-effort fallback
    return {"delta": str(obj)}


def _to_openai_json(obj: Any) -> Dict[str, Any]:
    # Extract a final text from LiteLLM response
    try:
        if hasattr(obj, "choices"):
            c0 = obj.choices[0]
            if hasattr(c0, "message") and getattr(c0.message, "content", None) is not None:
                return {"content": c0.message.content}
            if getattr(c0, "text", None) is not None:
                return {"content": c0.text}
    except Exception:
        pass
    return {"content": str(obj)}


def main():
    parser = argparse.ArgumentParser(description="Portal demo: LLM Gateway (Python)")
    parser.add_argument("--port", type=int, default=int(os.getenv("PORT", "8085")), help="local HTTP port")
    parser.add_argument("--host", type=str, default=os.getenv("HOST", "127.0.0.1"), help="bind host")
    parser.add_argument("--name", type=str, default="llm-gateway", help="display name for relay UI")
    parser.add_argument("--server-url", type=str, default=os.getenv("RELAY") or os.getenv("RELAY_URL"), help="relay websocket URL")
    # LLM configuration
    parser.add_argument("--engine", type=str, default=os.getenv("LLM_ENGINE") or "ollama", help="llm engine type: ollama | openai_compatible")
    parser.add_argument("--api-key", dest="api_key", type=str, default=os.getenv("LLM_API_KEY") or os.getenv("OPENAI_API_KEY") or "", help="api key for provider (if required)")
    parser.add_argument("--base-url", dest="base_url", type=str, default=os.getenv("LLM_BASE_URL") or "", help="provider base url (e.g., http://127.0.0.1:11434 or http://127.0.0.1:1234/v1)")
    parser.add_argument("--model", type=str, default=os.getenv("LLM_MODEL") or "", help="default model name override")
    parser.add_argument("--providers", type=str, default=os.getenv("LLM_PROVIDERS") or "", help="JSON array of providers; overrides engine/base/model/api_key")
    parser.add_argument("--debug", action="store_true", help="enable verbose LiteLLM debugging")
    args = parser.parse_args()

    # Build providers from CLI/env
    global PROVIDERS
    if args.providers:
        PROVIDERS = parse_providers_json(args.providers)
    else:
        PROVIDERS = build_providers(args.engine, args.api_key, args.base_url, args.model)

    # Optional LiteLLM debug
    if args.debug and litellm is not None:
        try:
            litellm._turn_on_debug()  # type: ignore[attr-defined]
            print("[llm-gateway] LiteLLM debug enabled")
        except Exception:
            pass

    httpd = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"LLM Gateway running on http://{args.host}:{args.port}")
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


if __name__ == "__main__":
    main()
