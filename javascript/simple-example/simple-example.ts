import http from "http";
import { portalTunnel } from "./tunnel.js";

// simple CLI args: --port, --host, --name, --server-url
function argValue(key: string): string | undefined {
  const argv = process.argv.slice(2);
  for (let i=0;i<argv.length;i++){
    const a = argv[i];
    if (a === `--${key}`) return argv[i+1];
    if (a.startsWith(`--${key}=`)) return a.split("=",2)[1];
  }
  return undefined;
}

const PORT = Number(argValue("port") || process.env.PORT || 8080);
const HOST = argValue("host") || "127.0.0.1";
const NAME = argValue("name") || "ts-test-tunnel";
const RELAY = argValue("server-url") || process.env.RELAY || process.env.RELAY_URL || "wss://portal.gosuda.org/relay";

const server = http.createServer((req, res) => {
  res.end("Hello, World!");
});

server.listen(PORT, HOST, () => {
  console.log(`Server is running on http://${HOST}:${PORT}`);
  portalTunnel({ port: PORT, host: HOST, name: NAME, relay: RELAY });
});
