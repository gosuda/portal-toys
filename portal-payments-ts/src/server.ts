import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { dirname, extname, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";

const distDir = dirname(fileURLToPath(import.meta.url));
const projectDir = dirname(distDir);
const publicDir = join(projectDir, "public");
const clientBundlePath = join(distDir, "client.js");
const protectedImagePath = join(publicDir, "assets", "unlocked-pass.png");

const port = numberFromEnv("PORT", 3000);
const host = textFromEnv("HOST", "0.0.0.0");
const paymentAmount = textFromEnv("PAYMENT_AMOUNT", "0.01");
const protectedPath = normalizeURLPath(textFromEnv("PROTECTED_PATH", "/paid"));
const paymentNetwork = textFromEnv("PAYMENT_NETWORK", "");
const paymentNetworkName = textFromEnv("PAYMENT_NETWORK_LABEL", networkLabel(paymentNetwork));

const contentTypes: Record<string, string> = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "application/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".png": "image/png"
};

const server = createServer((req, res) => {
  void route(req, res).catch((error: unknown) => {
    console.error(error);
    sendText(res, 500, "internal server error");
  });
});

server.listen(port, host, () => {
  console.log(`[portal-payments-ts] local server listening on http://${host}:${port}`);
  console.log(`[portal-payments-ts] portal upstream should use http://127.0.0.1:${port}`);
  console.log("[portal-payments-ts] expose it with npm run tunnel:testnet -- 0xYOUR_SUI_ADDRESS");
});

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

async function route(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const method = req.method ?? "GET";
  const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "127.0.0.1"}`);
  const path = normalizeURLPath(url.pathname);

  if (method !== "GET" && method !== "HEAD") {
    sendText(res, 405, "method not allowed", { Allow: "GET, HEAD" });
    return;
  }

  switch (path) {
    case "/":
    case "/index.html":
      sendHtml(res, renderIndex(url));
      return;
    case "/app.js":
      await sendFile(req, res, clientBundlePath, "application/javascript; charset=utf-8");
      return;
    case "/styles.css":
      await sendFile(req, res, join(publicDir, "styles.css"));
      return;
    case "/api/status":
      sendJson(res, {
        ok: true,
        app: "portal-payments-ts",
        payment: {
          protectedPath,
          amount: paymentAmount,
          asset: "USDC",
          network: paymentNetwork || "portal tunnel selected"
        },
        routes: recommendedRoutes()
      });
      return;
    case "/paid":
    case "/paid/":
      sendHtml(res, await renderPaid(req));
      return;
    default:
      sendText(res, 404, "not found");
  }
}

function renderIndex(url: URL): string {
  const config = {
    amount: paymentAmount,
    asset: "USDC",
    network: paymentNetwork,
    networkName: paymentNetworkName,
    preparePath: "/x402/prepare",
    protectedPath,
    routeSpec: `/paid=http://127.0.0.1:${port}/paid GET:${paymentAmount}`
  };
  const configJSON = escapeScriptJSON(JSON.stringify(config));
  const localURL = `${url.protocol}//${url.host}`;

  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Portal x402 Checkout</title>
  <link rel="stylesheet" href="/styles.css">
</head>
<body>
  <main class="shell">
    <section class="workspace" aria-labelledby="page-title">
      <div class="mast">
        <p class="eyebrow">Sui USDC x402</p>
        <h1 id="page-title">Portal x402 Checkout</h1>
        <p class="summary">A TypeScript HTTP app exposed through Portal paid routed HTTP.</p>
      </div>

      <div class="grid">
        <section class="panel checkout" aria-labelledby="checkout-title">
          <div class="section-head">
            <h2 id="checkout-title">Checkout</h2>
            <span class="pill">${escapeHtml(paymentAmount)} USDC</span>
          </div>

          <dl class="facts">
            <div>
              <dt>Protected route</dt>
              <dd>${escapeHtml(protectedPath)}</dd>
            </div>
            <div>
              <dt>Prepare endpoint</dt>
              <dd>/x402/prepare</dd>
            </div>
            <div>
              <dt>Network</dt>
              <dd>${escapeHtml(paymentNetworkName)}</dd>
            </div>
          </dl>

          <label class="field">
            <span>Wallet</span>
            <select id="walletSelect" disabled>
              <option>Loading wallets</option>
            </select>
          </label>

          <label class="field">
            <span>Account</span>
            <select id="accountSelect" disabled>
              <option>Connect wallet first</option>
            </select>
          </label>

          <div class="actions">
            <button id="unlockButton" class="primary" type="button" disabled>Pay and unlock</button>
            <button id="refreshButton" class="secondary" type="button">Refresh</button>
          </div>

          <p id="status" class="status" role="status">Waiting for Portal x402 helper.</p>
        </section>

        <section class="panel viewer" aria-labelledby="viewer-title">
          <div class="section-head">
            <h2 id="viewer-title">Protected Content</h2>
            <span id="viewerState" class="pill muted">Locked</span>
          </div>
          <div id="lockedView" class="locked-view">
            <div class="lock-mark">402</div>
            <p>Premium access pass</p>
          </div>
          <iframe id="paidFrame" title="Unlocked protected content" hidden></iframe>
        </section>
      </div>

      <section class="panel route-panel" aria-labelledby="route-title">
        <div class="section-head">
          <h2 id="route-title">Tunnel Route</h2>
          <span class="pill muted">localhost ${escapeHtml(String(port))}</span>
        </div>
        <code>${escapeHtml(config.routeSpec)}</code>
        <p class="note">Local origin: ${escapeHtml(localURL)}. The payment helper appears only on the public Portal tunnel origin.</p>
      </section>
    </section>
  </main>
  <script id="payment-config" type="application/json">${configJSON}</script>
  <script type="module" src="/app.js"></script>
</body>
</html>`;
}

async function renderPaid(req: IncomingMessage): Promise<string> {
  const image = await readFile(protectedImagePath);
  const imageData = image.toString("base64");
  const forwardedHost = headerValue(req, "x-forwarded-host");
  const forwardedPrefix = headerValue(req, "x-forwarded-prefix");
  const reachedViaTunnel = forwardedHost !== "";
  const accessLabel = reachedViaTunnel ? "Portal tunnel settled this request before upstream routing." : "Local direct request. Payment is enforced only through portal expose.";
  const issuedAt = new Date().toISOString();

  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f7f8fb; color: #17202a; }
    .receipt { display: grid; gap: 16px; padding: 18px; }
    img { width: 100%; display: block; border-radius: 8px; border: 1px solid #d9e0e8; }
    h1 { margin: 0; font-size: 22px; letter-spacing: 0; }
    p { margin: 0; line-height: 1.5; color: #40515f; }
    dl { display: grid; gap: 8px; margin: 0; padding: 12px; background: #ffffff; border: 1px solid #d9e0e8; border-radius: 8px; }
    div { display: flex; justify-content: space-between; gap: 12px; }
    dt { color: #667582; }
    dd { margin: 0; color: #17202a; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; overflow-wrap: anywhere; }
  </style>
</head>
<body>
  <article class="receipt">
    <img src="data:image/png;base64,${imageData}" alt="Unlocked premium access pass">
    <h1>Unlocked premium access pass</h1>
    <p>${escapeHtml(accessLabel)}</p>
    <dl>
      <div><dt>Issued</dt><dd>${escapeHtml(issuedAt)}</dd></div>
      <div><dt>Forwarded host</dt><dd>${escapeHtml(forwardedHost || "none")}</dd></div>
      <div><dt>Forwarded prefix</dt><dd>${escapeHtml(forwardedPrefix || "none")}</dd></div>
    </dl>
  </article>
</body>
</html>`;
}

function recommendedRoutes(): string[] {
  return [
    `/paid=http://127.0.0.1:${port}/paid GET:${paymentAmount}`,
    `/api=http://127.0.0.1:${port}/api`,
    `/=http://127.0.0.1:${port}`
  ];
}

async function sendFile(req: IncomingMessage, res: ServerResponse, filePath: string, contentType?: string): Promise<void> {
  const safePath = normalize(filePath);
  if (!safePath.startsWith(projectDir)) {
    sendText(res, 403, "forbidden");
    return;
  }

  const fileStat = await stat(safePath).catch(() => null);
  if (!fileStat?.isFile()) {
    sendText(res, 404, "not found");
    return;
  }

  res.writeHead(200, {
    "Content-Type": contentType ?? contentTypes[extname(safePath).toLowerCase()] ?? "application/octet-stream",
    "Content-Length": String(fileStat.size),
    "Cache-Control": "no-store"
  });
  if (req.method === "HEAD") {
    res.end();
    return;
  }
  createReadStream(safePath).pipe(res);
}

function sendHtml(res: ServerResponse, html: string): void {
  sendText(res, 200, html, {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store"
  });
}

function sendJson(res: ServerResponse, value: unknown): void {
  sendText(res, 200, JSON.stringify(value, null, 2), {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store"
  });
}

function sendText(res: ServerResponse, status: number, body: string, headers: Record<string, string> = {}): void {
  res.writeHead(status, {
    "Content-Type": "text/plain; charset=utf-8",
    "Content-Length": String(Buffer.byteLength(body)),
    ...headers
  });
  res.end(body);
}

function headerValue(req: IncomingMessage, name: string): string {
  const value = req.headers[name];
  if (Array.isArray(value)) {
    return value[0] ?? "";
  }
  return value ?? "";
}

function numberFromEnv(name: string, fallback: number): number {
  const value = Number(process.env[name]);
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

function textFromEnv(name: string, fallback: string): string {
  const value = process.env[name]?.trim();
  return value ? value : fallback;
}

function networkLabel(network: string): string {
  switch (network.trim().toLowerCase()) {
    case "sui:mainnet":
      return "Sui Mainnet";
    case "sui:testnet":
      return "Sui Testnet";
    default:
      return "Portal tunnel selected";
  }
}

function normalizeURLPath(path: string): string {
  const trimmed = path.trim();
  if (trimmed === "" || trimmed === "/") {
    return "/";
  }
  return `/${trimmed.replace(/^\/+/, "").replace(/\/+$/, "")}`;
}

function escapeScriptJSON(value: string): string {
  return value.replace(/</g, "\\u003c").replace(/>/g, "\\u003e").replace(/&/g, "\\u0026");
}

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (char) => {
    switch (char) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case "\"":
        return "&quot;";
      default:
        return "&#39;";
    }
  });
}

function shutdown(): void {
  server.close(() => process.exit(0));
}
