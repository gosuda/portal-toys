import { spawn, ChildProcess } from "child_process";

export type LogLevel = "silent" | "error" | "verbose";

export interface PortalTunnelOptions {
  port: number;
  name: string;
  relay: string;
  logLevel?: LogLevel;
}

export async function portalTunnel(options: PortalTunnelOptions): Promise<ChildProcess> {
  const { port, name, relay, logLevel = "verbose" } = options;

  if (!port || !name || !relay) {
    const missing: Array<"port" | "name" | "relay"> = [];
    if (!port) missing.push("port");
    if (!name) missing.push("name");
    if (!relay) missing.push("relay");
    throw new Error(`Missing required options: ${missing.join(", ")}`);
  }

  // portal-tunnel expose 8080 --name http-example --relay wss://portal.gosuda.org/relay
  const args: string[] = ["expose", port.toString(), "--name", name, "--relay", relay];

  const tunnelProcess = spawn("portal-tunnel", args);

  if (logLevel === "verbose") {
    tunnelProcess.stdout?.on("data", (data: Buffer) => {
      console.log(`[Portal Tunnel] ${data.toString().trim()}`);
    });
  }

  if (logLevel === "error" || logLevel === "verbose") {
    tunnelProcess.stderr?.on("data", (data: Buffer) => {
      console.error(`[Portal Tunnel Error] ${data.toString().trim()}`);
    });
  }

  tunnelProcess.on("error", (error: NodeJS.ErrnoException) => {
    if (error.code === "ENOENT") {
      console.error("\n╔═══════════════════════════════════════════════════════════════════╗");
      console.error("║                   Portal Tunnel Not Found                         ║");
      console.error("╠═══════════════════════════════════════════════════════════════════╣");
      console.error("║ portal-tunnel is not installed or not in PATH.                    ║");
      console.error("║                                                                   ║");
      console.error("║ Please install it using:                                          ║");
      console.error("║                                                                   ║");
      console.error("║   go install gosuda.org/portal/cmd/portal-tunnel@latest           ║");
      console.error("║                                                                   ║");
      console.error("╚═══════════════════════════════════════════════════════════════════╝\n");
    } else {
      console.error(`\nPortal Tunnel Execute Error: ${error.message}`);
      if (error.code) {
        console.error(`\tError Code: ${error.code}`);
      }
    }
  });

  if (logLevel === "error" || logLevel === "verbose") {
    tunnelProcess.on("close", (code: number | null) => {
      if (code !== 0 && code !== null) {
        console.error(`\nPortal Tunnel exited with code: ${code}`);
      }
    });
  }

  return tunnelProcess;
}
