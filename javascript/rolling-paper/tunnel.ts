import { spawn, ChildProcess } from "child_process";
import fs from "fs";
import path from "path";

export type Tunnel = ChildProcess | undefined;

export type TunnelOptions = {
  enabled?: boolean; // default: true
  port?: number;     // default: env PORT or 8080
  host?: string;     // default: 127.0.0.1
  relay?: string;    // default: env RELAY|RELAY_URL or wss://portal.gosuda.org/relay
  name?: string;     // default: env TUNNEL_NAME or js-rolling-paper
  bin?: string;      // default: env TUNNEL_BIN|PORTAL_TUNNEL_BIN or auto-resolve
};

export function startTunnel(opts: TunnelOptions = {}): Tunnel {
  const enabled = parseBool(process.env.TUNNEL_ENABLED, true);
  const shouldRun = opts.enabled ?? enabled;
  if (!shouldRun) {
    console.log("[tunnel] disabled (TUNNEL_ENABLED=false)");
    return undefined;
  }

  const port = opts.port ?? Number(process.env.PORT || 8080);
  const host = opts.host ?? "127.0.0.1";
  const relay = opts.relay ?? (process.env.RELAY || process.env.RELAY_URL);
  const name = opts.name ?? (process.env.TUNNEL_NAME || "js-rolling-paper");
  const bin = opts.bin ?? (process.env.TUNNEL_BIN || process.env.PORTAL_TUNNEL_BIN || resolveTunnelBin());

  const args = [
    "expose",
    "--port", String(port),
    "--host", host,
    "--name", name,
    "--relay", relay,
  ];
  console.log(`[tunnel] using binary: ${bin}`);
  const proc = spawn(bin, args);
  proc.stdout?.on("data", (d) => console.log(`[tunnel] ${d.toString().trim()}`));
  proc.stderr?.on("data", (d) => console.error(`[tunnel] ${d.toString().trim()}`));
  proc.on("error", (err: NodeJS.ErrnoException) => {
    if (err.code === "ENOENT") {
      console.error("\nportal-tunnel not found. Install via: make tunnel-install\n");
    } else {
      console.error(`\nportal-tunnel error: ${err.message}`);
    }
  });
  proc.on("close", (code) => {
    if (code !== 0 && code !== null) console.error(`\nportal-tunnel exited with code: ${code}`);
  });
  return proc;
}

export function stopTunnel(proc: Tunnel) {
  if (proc && !proc.killed) {
    proc.kill();
  }
}

function parseBool(v: string | undefined, dflt: boolean): boolean {
  if (!v) return dflt;
  const s = v.toLowerCase().trim();
  if (["1", "true", "yes", "on"].includes(s)) return true;
  if (["0", "false", "no", "off"].includes(s)) return false;
  return dflt;
}

function resolveTunnelBin(): string {
  const exe = process.platform === "win32" ? "portal-tunnel.exe" : "portal-tunnel";
  // try common locations relative to repo or cwd
  const repoRoot = path.resolve(process.cwd(), "..", "..");
  const candidates = [
    path.join(repoRoot, "bin", exe),
    path.join(process.cwd(), "bin", exe),
    path.join(repoRoot, exe),
  ];
  for (const p of candidates) {
    try { if (fs.existsSync(p)) return p; } catch {}
  }
  return exe; // PATH fallback
}
