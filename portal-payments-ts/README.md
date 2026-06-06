# Portal Payments TypeScript

Pure TypeScript example for Portal 2.3.2 paid routed HTTP. The app runs a local Node HTTP server on port 3000. The Portal CLI owns the tunnel, `/x402/client.js`, `/x402/prepare`, payment settlement, and route-level protection.

## Install

```bash
npm install
npm run build
HOST=0.0.0.0 PAYMENT_NETWORK=sui:testnet npm start
```

Open `http://127.0.0.1:3000` for the local app shell. The payment button is enabled only when the app is opened through a Portal tunnel because `/x402/client.js` is served by `portal expose`, not by this TypeScript server.

## Install Portal CLI

```bash
curl -fsSL https://github.com/gosuda/portal-tunnel/releases/latest/download/install.sh | bash
```

PowerShell:

```powershell
$ProgressPreference = 'SilentlyContinue'
irm https://github.com/gosuda/portal-tunnel/releases/latest/download/install.ps1 | iex
```

## Expose Paid Routes

One-command WSL development run:

```bash
cd /mnt/c/workspace/relaydns-toys/portal-payments-ts
npm run dev:tunnel -- 0xYOUR_SUI_ADDRESS
```

That command builds TypeScript, starts the local server, waits for `/api/status`, runs `portal expose`, and stops the local server when the tunnel exits.

Testnet:

```bash
PAY_TO=0xYOUR_SUI_ADDRESS
portal expose --name portal-payments-ts \
  --http-route "/paid=http://127.0.0.1:3000/paid GET:0.01" \
  --http-route "/api=http://127.0.0.1:3000/api" \
  --http-route "/=http://127.0.0.1:3000" \
  --x402-testnet \
  --x402-pay-to "$PAY_TO"
```

PowerShell:

```powershell
$PayTo = "0xYOUR_SUI_ADDRESS"
portal expose --name portal-payments-ts `
  --http-route "/paid=http://127.0.0.1:3000/paid GET:0.01" `
  --http-route "/api=http://127.0.0.1:3000/api" `
  --http-route "/=http://127.0.0.1:3000" `
  --x402-testnet `
  --x402-pay-to $PayTo
```

You can also use the npm wrapper:

```bash
npm run tunnel:testnet -- 0xYOUR_SUI_ADDRESS
```

`tunnel:testnet` starts both the local TypeScript server and Portal. If you already started the server yourself and only want Portal, use:

```bash
npm run expose:testnet -- 0xYOUR_SUI_ADDRESS
```

For mainnet, remove `--x402-testnet` or run:

```bash
npm run tunnel:mainnet -- 0xYOUR_SUI_ADDRESS
```

## How It Works

- `/` is the public TypeScript app shell.
- `/api/status` is a public JSON route.
- `/paid` is protected by Portal routed HTTP with `GET:0.01`.
- The browser dynamically imports `/x402/client.js` from the public tunnel origin.
- `x402Fetch("/paid")` prepares and signs the Sui USDC payment, sends `X-PAYMENT`, and receives the protected HTML only after the tunnel settles the payment.
- Settlement failures are returned to the browser as a 402 payment error. Detailed Sui dry-run reasons, such as insufficient testnet USDC balance, are logged by the local `portal expose` process.

The upstream path includes `/paid` in `http://127.0.0.1:3000/paid` because Portal strips the public route prefix before proxying.

## Configuration

The local display values can be adjusted with environment variables:

```bash
HOST=0.0.0.0 PORT=3000 PAYMENT_AMOUNT=0.01 PAYMENT_NETWORK=sui:testnet npm start
```

If you change `PAYMENT_AMOUNT`, change the `GET:0.01` amount in the `portal expose` route as well. The tunnel route is the source of truth for the actual payment requirement.

## Bad Gateway Checklist

`bad gateway` means Portal reached the tunnel, but the tunnel could not reach the local upstream. In WSL, check these in order:

```bash
cd /mnt/c/workspace/relaydns-toys/portal-payments-ts

npm run build
HOST=0.0.0.0 PAYMENT_NETWORK=sui:testnet npm start
```

Then in another WSL terminal:

```bash
curl -i http://127.0.0.1:3000/
curl -i http://127.0.0.1:3000/api/status
curl -i http://127.0.0.1:3000/paid
portal version
portal expose --help | grep x402
```

If those local `curl` commands fail, the Node server is not running on the same WSL side as `portal expose`. If `portal expose --help` does not show `--x402-testnet`, reinstall the latest Portal CLI and make sure `command -v portal` points to that install.
