import http from "http";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { startTunnel, stopTunnel, type Tunnel } from "./tunnel.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const PORT = Number(process.env.PORT || 8080);
const PUBLIC_DIR = path.join(__dirname, "public");

type Note = {
  id: string;
  author: string;
  text: string;
  color: string;
  at: number;
};

type EventItem = { type: "created" | "deleted" | "voted"; at: number; note?: Note; id?: string; votes?: number };

const notes: Note[] = [];
// Track delete votes per note id; delete when votes >= REQUIRED_VOTES
const deleteVotes = new Map<string, number>();
const REQUIRED_VOTES = 3;
const sseClients = new Map<string, http.ServerResponse>();
const backlog: EventItem[] = [];
const MAX_BACKLOG = 200;

function pushEvent(ev: EventItem) {
  backlog.push(ev);
  if (backlog.length > MAX_BACKLOG) backlog.splice(0, backlog.length - MAX_BACKLOG);
  const payload = `data: ${JSON.stringify(ev)}\n\n`;
  for (const res of sseClients.values()) {
    try { res.write(payload); } catch {}
  }
}

function writeJSON(res: http.ServerResponse, status: number, body: unknown) {
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-cache, no-store, must-revalidate",
    "Pragma": "no-cache",
    "Expires": "0",
    "Access-Control-Allow-Origin": "*",
  });
  res.end(JSON.stringify(body));
}

// HTTP server
const server = http.createServer((req, res) => {
  const url = req.url || "/";

  // SSE stream
  if (url.startsWith("/events")) {
    res.writeHead(200, {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      "Connection": "keep-alive",
      "Access-Control-Allow-Origin": "*",
      "X-Accel-Buffering": "no",
    });
    try { res.socket?.setNoDelay(true); } catch {}
    try { (res as any).flushHeaders?.(); } catch {}
    res.write("retry: 2000\n\n");
    res.write(": open\n\n");
    const id = Math.random().toString(36).slice(2);
    sseClients.set(id, res);
    req.on("close", () => sseClients.delete(id));
    return;
  }

  // List notes
  if (url === "/api/notes" && req.method === "GET") {
    // include current delete votes so clients can display (x/REQUIRED_VOTES)
    const withVotes = notes.map((n) => ({ ...n, votes: deleteVotes.get(n.id) ?? 0, required: REQUIRED_VOTES }));
    writeJSON(res, 200, withVotes);
    return;
  }

  // Create note
  if (url === "/api/notes" && req.method === "POST") {
    let body = "";
    req.on("data", (c) => { body += c; if (body.length > 1_000_000) req.destroy(); });
    req.on("end", () => {
      try {
        const data = JSON.parse(body || "{}");
        const author = (typeof data.author === "string" ? data.author : "").trim().slice(0, 24) || "anon";
        const text = (typeof data.text === "string" ? data.text : "").trim().slice(0, 500);
        const color = (typeof data.color === "string" ? data.color : "").trim().slice(0, 16) || "#fff"; // default: white
        if (!text) { writeJSON(res, 400, { error: "empty" }); return; }
        const note: Note = { id: Math.random().toString(36).slice(2), author, text, color, at: Date.now() };
        notes.push(note);
        writeJSON(res, 201, note);
        pushEvent({ type: "created", at: Date.now(), note });
      } catch {
        writeJSON(res, 400, { error: "bad json" });
      }
    });
    return;
  }

  // Delete note
  if (url.startsWith("/api/notes/") && req.method === "DELETE") {
    const id = url.split("/").pop() || "";
    const idx = notes.findIndex((n) => n.id === id);
    if (idx < 0) { writeJSON(res, 404, { error: "not found" }); return; }
    const votes = (deleteVotes.get(id) ?? 0) + 1;
    deleteVotes.set(id, votes);
    if (votes >= REQUIRED_VOTES) {
      // Perform deletion and clear vote counter
      notes.splice(idx, 1);
      deleteVotes.delete(id);
      writeJSON(res, 204, {});
      pushEvent({ type: "deleted", at: Date.now(), id });
    } else {
      writeJSON(res, 202, { votes, required: REQUIRED_VOTES });
      // notify others of vote progress
      pushEvent({ type: "voted", at: Date.now(), id, votes });
    }
    return;
  }

  // Poll incremental events
  if (url.startsWith("/api/poll") && req.method === "GET") {
    let since = 0;
    try { const q = new URL(req.url || "", "http://x").searchParams; since = Number(q.get("since") || 0) || 0; } catch {}
    const out = backlog.filter((e) => e.at > since);
    writeJSON(res, 200, out);
    return;
  }

  // Static files
  if (url === "/" || url === "/index.html") {
    const file = path.join(PUBLIC_DIR, "index.html");
    fs.createReadStream(file).on("error", () => { res.writeHead(500); res.end("Missing page"); }).pipe(res);
    return;
  }

  res.writeHead(404);
  res.end("Not found");
});

server.listen(PORT, () => {
  console.log(`Rolling Paper running on http://localhost:${PORT}`);
  tunnelProc = startTunnel({ port: PORT });
});

// graceful shutdown and tunnel cleanup
let tunnelProc: Tunnel;
function cleanup() { stopTunnel(tunnelProc); }

process.on("SIGINT", () => { cleanup(); process.exit(); });
process.on("SIGTERM", () => { cleanup(); process.exit(); });
process.on("exit", cleanup);
