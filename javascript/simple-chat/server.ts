import http from "http";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { startTunnel, stopTunnel, type Tunnel } from "./tunnel.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const PORT = Number(process.env.PORT || 8080);
const PUBLIC_DIR = process.env.PUBLIC_DIR || path.join(__dirname, "public");

type Message = { type: "system" | "chat"; text: string; name?: string; at: number };
const clients = new Map<string, http.ServerResponse>();
const backlog: Message[] = [];
const MAX_BACKLOG = 100;
type Waiter = { since: number; res: http.ServerResponse; timer: NodeJS.Timeout };
const waiters: Waiter[] = [];

function sseWrite(res: http.ServerResponse, msg: Message) {
  res.write(`data: ${JSON.stringify(msg)}\n\n`);
}
function broadcast(msg: Message) {
  for (const res of clients.values()) {
    try { sseWrite(res, msg); } catch {}
  }
  backlog.push(msg);
  if (backlog.length > MAX_BACKLOG) backlog.splice(0, backlog.length - MAX_BACKLOG);
  // Wake long-poll waiters
  if (waiters.length > 0) {
    for (let i = waiters.length - 1; i >= 0; i--) {
      const w = waiters[i];
      const out = backlog.filter(m => (m.at || 0) > w.since);
      if (out.length > 0) {
        clearTimeout(w.timer);
        try {
          w.res.writeHead(200, {
            "Content-Type": "application/json; charset=utf-8",
            "Cache-Control": "no-cache, no-store, must-revalidate",
            "Pragma": "no-cache",
            "Expires": "0",
            "Access-Control-Allow-Origin": "*",
          });
          w.res.end(JSON.stringify(out));
        } catch {}
        waiters.splice(i, 1);
      }
    }
  }
}

// HTTP server (SSE + static)
const server = http.createServer((req, res) => {
  const url = req.url || "/";

  // SSE endpoint
  if (url.startsWith("/events")) {
    res.writeHead(200, {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      "Connection": "keep-alive",
      "Access-Control-Allow-Origin": "*",
      // hint some proxies (e.g., nginx) to avoid buffering SSE
      "X-Accel-Buffering": "no",
    });
    // Disable Nagle's algorithm for lower latency small chunks
    try { res.socket?.setNoDelay(true); } catch {}
    // Flush headers immediately
    try { (res as any).flushHeaders?.(); } catch {}
    // reconnection delay hint and a comment line to prime the stream
    res.write("retry: 2000\n\n");
    res.write(": open\n\n");
    const id = Math.random().toString(36).slice(2);
    clients.set(id, res);
    // send backlog
    for (const m of backlog) {
      try { sseWrite(res, m); } catch {}
    }
    // initial system line
    sseWrite(res, { type: "system", text: "connected", at: Date.now() });
    req.on("close", () => {
      clients.delete(id);
    });
    return;
  }

  // Polling endpoint: returns messages after given timestamp
  if (url.startsWith("/poll") && req.method === "GET") {
    // parse since param
    let since = 0;
    try {
      const q = new URL(req.url || "", "http://x").searchParams;
      since = Number(q.get("since") || 0) || 0;
    } catch {}
    const out = backlog.filter((m) => (m.at || 0) > since);
    if (out.length > 0) {
      res.writeHead(200, {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-cache, no-store, must-revalidate",
        "Pragma": "no-cache",
        "Expires": "0",
        "Access-Control-Allow-Origin": "*",
      });
      res.end(JSON.stringify(out));
      return;
    }
    // No data yet: hold the request (long-poll) up to 25s
    try { req.socket?.setNoDelay(true); } catch {}
    const timer = setTimeout(() => {
      try {
        res.writeHead(200, {
          "Content-Type": "application/json; charset=utf-8",
          "Cache-Control": "no-cache, no-store, must-revalidate",
          "Pragma": "no-cache",
          "Expires": "0",
          "Access-Control-Allow-Origin": "*",
        });
        res.end("[]");
      } catch {}
      const idx = waiters.indexOf(w);
      if (idx >= 0) waiters.splice(idx, 1);
    }, 25000);
    const w: Waiter = { since, res, timer };
    waiters.push(w);
    req.on("close", () => {
      clearTimeout(timer);
      const idx = waiters.indexOf(w);
      if (idx >= 0) waiters.splice(idx, 1);
    });
    return;
  }

  // Send endpoint
  if (url === "/send" && req.method === "POST") {
    let body = "";
    req.on("data", (chunk) => { body += chunk; if (body.length > 1_000_000) req.destroy(); });
    req.on("end", () => {
      try {
        const data = JSON.parse(body || "{}");
        const name = (typeof data.name === "string" ? data.name : "").trim() || "anon";
        const text = (typeof data.text === "string" ? data.text : "").slice(0, 500).trim();
        if (!text) { res.writeHead(400); res.end("empty"); return; }
        broadcast({ type: "chat", name, text, at: Date.now() });
        res.writeHead(204); res.end();
      } catch {
        res.writeHead(400); res.end("bad json");
      }
    });
    return;
  }

  // Static files
  if (url === "/" || url === "/index.html") {
    const file = path.join(PUBLIC_DIR, "index.html");
    fs.createReadStream(file).on("error", () => {
      res.writeHead(500);
      res.end("Missing client file");
    }).pipe(res);
    return;
  }
  if (url === "/client.js") {
    const file = path.join(PUBLIC_DIR, "client.js");
    fs.createReadStream(file).on("error", () => {
      res.writeHead(404);
      res.end("Not found");
    }).pipe(res);
    return;
  }

  res.writeHead(404);
  res.end("Not found");
});

// Periodic SSE heartbeat to keep proxies from closing idle connections
setInterval(() => {
  const msg: Message = { type: "system", text: "ping", at: Date.now() };
  for (const res of clients.values()) {
    try { res.write(`event: ping\n`); sseWrite(res, msg); } catch {}
  }
}, 30000);

let tunnelProc: Tunnel;
function cleanup() { stopTunnel(tunnelProc); }

server.listen(PORT, () => {
  console.log(`Simple chat running on http://localhost:${PORT}`);
  tunnelProc = startTunnel({ port: PORT });
});

process.on("SIGINT", () => { cleanup(); process.exit(); });
process.on("SIGTERM", () => { cleanup(); process.exit(); });
process.on("exit", cleanup);
