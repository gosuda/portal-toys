import http from "http";
import { portalTunnel } from "./tunnel.js";

const PORT = 8080;

const server = http.createServer((req, res) => {
  res.end("Hello, World!");
});

server.listen(PORT, () => {
  console.log(`Server is running on port ${PORT}`);

  portalTunnel({
    port: PORT,
    name: "ts-test-tunnel",
    relay: "ws://localhost:4017/relay",
    // logLevel: "silent",
  });
});
