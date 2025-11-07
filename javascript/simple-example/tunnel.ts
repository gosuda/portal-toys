import { spawn, ChildProcess } from "child_process";
import fs from "fs";
import path from "path";

export type LogLevel = "silent" | "error" | "verbose";

export interface PortalTunnelOptions {
  port: number;
  name: string;
  relay?: string;
  host?: string; // default: 127.0.0.1
  bin?: string;
  logLevel?: LogLevel;
}

function resolvePortalTunnel(): string {
  const exe = process.platform === "win32" ? "portal-tunnel.exe" : "portal-tunnel";
  const cwd = process.cwd();
  const repoRoot = path.resolve(cwd, "..", "..");
  const envBin = process.env.TUNNEL_BIN || process.env.PORTAL_TUNNEL_BIN;
  const candidates = [
    envBin,
    path.join(repoRoot, "bin", exe),
    path.join(cwd, "bin", exe),
    exe, // PATH fallback
  ].filter(Boolean) as string[];
  for (const p of candidates) {
    try { if (fs.existsSync(p)) return p; } catch {}
  }
  return exe;
}

export async function portalTunnel(options: PortalTunnelOptions): Promise<ChildProcess> {
  const { port, name, logLevel = "verbose" } = options;
  const relay = options.relay || process.env.RELAY || process.env.RELAY_URL;
  const host = options.host || "127.0.0.1";
  if (!port || !name || !relay) {
    const missing: Array<"port" | "name" | "relay"> = [];
    if (!port) missing.push("port");
    if (!name) missing.push("name");
    if (!relay) missing.push("relay");
    throw new Error(`Missing required options: ${missing.join(", ")}`);
  }

  const bin = options.bin || resolvePortalTunnel();
  const args: string[] = ["expose", "--port", String(port), "--host", host, "--name", name, "--relay", relay];
  const proc = spawn(bin, args);

  if (logLevel === "verbose") {
    proc.stdout?.on("data", (data: Buffer) => {
      console.log(`[Portal Tunnel] ${data.toString().trim()}`);
    });
  }
  if (logLevel === "error" || logLevel === "verbose") {
    proc.stderr?.on("data", (data: Buffer) => {
      console.error(`[Portal Tunnel Error] ${data.toString().trim()}`);
    });
  }
  proc.on("error", (error: NodeJS.ErrnoException) => {
    if (error.code === "ENOENT") {
      console.error("\nportal-tunnel not found. Install via: make tunnel-install\n");
    } else {
      console.error(`\nPortal Tunnel Execute Error: ${error.message}`);
      if (error.code) console.error(`\tError Code: ${error.code}`);
    }
  });
  if (logLevel === "error" || logLevel === "verbose") {
    proc.on("close", (code: number | null) => {
      if (code !== 0 && code !== null) console.error(`\nPortal Tunnel exited with code: ${code}`);
    });
  }
  return proc;
}
