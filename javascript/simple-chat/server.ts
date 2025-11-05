import http from "http";
import path from "path";
import fs from "fs";
import { fileURLToPath } from "url";
import { spawn, ChildProcess } from "child_process";
import { WebSocketServer } from "ws";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Configuration
const PORT = Number(process.env.PORT || 8080);
const TUNNEL_NAME = process.env.TUNNEL_NAME || "js-simple-chat";
const RELAY = process.env.RELAY || "wss://portal.gosuda.org/relay";

// HTTP server (serves a minimal client)
const server = http.createServer((req, res) => {
  const url = req.url || "/";
  if (url === "/" || url === "/index.html") {
    const file = path.join(__dirname, "public", "index.html");
    fs.createReadStream(file).on("error", () => {
      res.writeHead(500);
      res.end("Missing client file");
    }).pipe(res);
    return;
  }
  if (url === "/client.js") {
    const file = path.join(__dirname, "public", "client.js");
    fs.createReadStream(file).on("error", () => {
      res.writeHead(404);
      res.end("Not found");
    }).pipe(res);
    return;
  }

  res.writeHead(404);
  res.end("Not found");
});

// WebSocket chat
type Client = {
  id: string;
  ws: import("ws").WebSocket;
  name?: string;
};

const wss = new WebSocketServer({ server, path: "/ws" });
const clients = new Map<string, Client>();

function broadcast(data: unknown) {
  const payload = JSON.stringify(data);
  for (const { ws } of clients.values()) {
    if (ws.readyState === ws.OPEN) ws.send(payload);
  }
}

wss.on("connection", (ws, req) => {
  console.log(`WS connected from ${req.socket.remoteAddress}`);
  const id = Math.random().toString(36).slice(2);
  const client: Client = { id, ws };
  clients.set(id, client);

  ws.on("message", (raw) => {
    try {
      const msg = JSON.parse(raw.toString());
      if (msg.type === "hello" && typeof msg.name === "string") {
        client.name = msg.name.slice(0, 20);
        broadcast({ type: "system", text: `${client.name} joined`, at: Date.now() });
        return;
      }
      if (msg.type === "chat" && typeof msg.text === "string") {
        const name = client.name || "anon";
        broadcast({ type: "chat", name, text: msg.text.slice(0, 500), at: Date.now() });
        return;
      }
    } catch {}
  });

  ws.on("close", (code, reason) => {
    console.log(`WS closed: code=${code} reason=${reason.toString()}`);
    clients.delete(id);
    if (client.name) broadcast({ type: "system", text: `${client.name} left`, at: Date.now() });
  });
});

// Optional: spawn portal-tunnel child process
let tunnelProc: ChildProcess | undefined;

function resolveTunnelBin(): string {
  const exe = process.platform === "win32" ? "portal-tunnel.exe" : "portal-tunnel";
  const repoRoot = path.resolve(__dirname, "..", "..");
  const cwd = process.cwd();
  const fromEnv = process.env.PORTAL_TUNNEL_BIN && process.env.PORTAL_TUNNEL_BIN.trim();

  const candidates = [
    fromEnv,
    path.join(repoRoot, "bin", exe),
    path.join(cwd, "bin", exe),
    path.join(repoRoot, exe),
  ].filter(Boolean) as string[];

  for (const p of candidates) {
    try {
      if (fs.existsSync(p)) return p;
    } catch {}
  }
  return "portal-tunnel"; // fallback to PATH
}

function startTunnel() {
  const bin = resolveTunnelBin();
  const args = ["expose", "--port", String(PORT), "--host", "127.0.0.1", "--name", TUNNEL_NAME, "--relay", RELAY];
  console.log(`[tunnel] using binary: ${bin}`);
  tunnelProc = spawn(bin, args);
  tunnelProc.stdout?.on("data", (d) => console.log(`[tunnel] ${d.toString().trim()}`));
  tunnelProc.stderr?.on("data", (d) => console.error(`[tunnel] ${d.toString().trim()}`));
  tunnelProc.on("error", (err: NodeJS.ErrnoException) => {
    if (err.code === "ENOENT") {
      console.error("\nportal-tunnel not found. Install via: make tunnel-install\n");
    } else {
      console.error(`\nportal-tunnel error: ${err.message}`);
    }
  });
  tunnelProc.on("close", (code) => {
    if (code !== 0 && code !== null) console.error(`\nportal-tunnel exited with code: ${code}`);
  });
}

function cleanup() {
  if (tunnelProc && !tunnelProc.killed) {
    tunnelProc.kill();
  }
}

server.listen(PORT, () => {
  console.log(`Simple chat running on http://localhost:${PORT}`);
  startTunnel();
});

process.on("SIGINT", () => { cleanup(); process.exit(); });
process.on("SIGTERM", () => { cleanup(); process.exit(); });
process.on("exit", cleanup);
