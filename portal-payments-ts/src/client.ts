interface PaymentConfig {
  amount: string;
  asset: string;
  network: string;
  networkName: string;
  preparePath: string;
  protectedPath: string;
  routeSpec: string;
}

interface SuiAccount {
  address: string;
  chains?: string[];
}

interface SuiWallet {
  id: string;
  name: string;
  accounts(chain?: string): Promise<SuiAccount[]>;
  connect(chain?: string, address?: string): Promise<SuiAccount>;
  signTransaction(account: SuiAccount, transaction: unknown, chain?: string): Promise<unknown>;
}

interface X402PaymentEvent {
  type: string;
  message: string;
  data?: Record<string, unknown>;
}

interface X402FetchOptions {
  wallet?: SuiWallet;
  account?: SuiAccount;
  network?: string;
  preparePath?: string;
  path?: string;
  onEvent?: (event: X402PaymentEvent) => void;
  onStatus?: (message: string) => void;
  signal?: AbortSignal;
}

interface PortalX402Client {
  getSuiWallets(options?: { network?: string }): SuiWallet[];
  onSuiWalletChange(callback: () => void): () => void;
  x402Fetch(input: RequestInfo | URL, init?: RequestInit, options?: X402FetchOptions): Promise<Response>;
}

interface X402PaymentRequiredResponse {
  x402Version?: number;
  error?: string;
  resource?: {
    url?: string;
  };
  accepts?: Array<{
    network?: string;
    asset?: string;
    amount?: string;
    payTo?: string;
    extra?: {
      asset?: string;
      assetTransferMethod?: string;
    };
  }>;
}

const config = readConfig();
const walletSelect = element<HTMLSelectElement>("walletSelect");
const accountSelect = element<HTMLSelectElement>("accountSelect");
const unlockButton = element<HTMLButtonElement>("unlockButton");
const refreshButton = element<HTMLButtonElement>("refreshButton");
const statusEl = element<HTMLElement>("status");
const paidFrame = element<HTMLIFrameElement>("paidFrame");
const lockedView = element<HTMLElement>("lockedView");
const viewerState = element<HTMLElement>("viewerState");

let portalClient: PortalX402Client | null = null;
let wallets: SuiWallet[] = [];
let connectedWalletIndex = "";
let accounts: SuiAccount[] = [];
let activePayerAddress = "";
let unsubscribeWalletChange: (() => void) | null = null;

void init();

async function init(): Promise<void> {
  refreshButton.addEventListener("click", () => {
    void refreshWallets();
  });
  walletSelect.addEventListener("change", resetAccounts);
  unlockButton.addEventListener("click", () => {
    void unlock();
  });

  setStatus("Loading Portal x402 helper.");
  await refreshWallets();
}

async function loadPortalClient(): Promise<PortalX402Client> {
  if (portalClient) {
    return portalClient;
  }
  const moduleURL = new URL("/x402/client.js", window.location.href).href;
  portalClient = await import(moduleURL) as PortalX402Client;
  unsubscribeWalletChange?.();
  unsubscribeWalletChange = portalClient.onSuiWalletChange(() => {
    void refreshWallets();
  });
  return portalClient;
}

async function refreshWallets(): Promise<void> {
  try {
    const client = await loadPortalClient();
    wallets = client.getSuiWallets({ network: config.network || undefined });
    walletSelect.replaceChildren(...walletOptions(wallets));
    walletSelect.disabled = wallets.length === 0;
    unlockButton.disabled = wallets.length === 0;
    unlockButton.textContent = "Pay and unlock";
    resetAccounts();
    setStatus(wallets.length > 0 ? "Select a wallet." : "No Sui wallet extension detected.");
  } catch (error) {
    wallets = [];
    walletSelect.replaceChildren(option("", "Open through Portal tunnel"));
    walletSelect.disabled = true;
    accountSelect.disabled = true;
    unlockButton.disabled = true;
    setStatus(errorMessage(error, "Portal x402 helper is not available on this origin."));
  }
}

async function unlock(): Promise<void> {
  setBusy(true);
  try {
    const client = await loadPortalClient();
    const connection = await selectedConnection();
    if (!connection) {
      return;
    }
    activePayerAddress = connection.account.address;

    setStatus("Preparing payment.");
    lockedView.hidden = true;
    paidFrame.hidden = false;
    viewerState.textContent = "Paying";

    const response = await client.x402Fetch(config.protectedPath, {
      method: "GET",
      headers: { Accept: "text/html" }
    }, {
      wallet: connection.wallet,
      account: connection.account,
      network: config.network || undefined,
      preparePath: config.preparePath,
      onEvent: updatePaymentStatus
    });

    if (!response.ok) {
      throw new Error(await paymentErrorMessage(response));
    }

    paidFrame.srcdoc = await response.text();
    viewerState.textContent = "Unlocked";
    setStatus(`Payment complete for ${config.amount} ${config.asset}.`);
  } catch (error) {
    lockedView.hidden = false;
    paidFrame.hidden = true;
    paidFrame.removeAttribute("srcdoc");
    viewerState.textContent = "Locked";
    setStatus(errorMessage(error, "Payment failed."));
  } finally {
    setBusy(false);
  }
}

async function selectedConnection(): Promise<{ wallet: SuiWallet; account: SuiAccount } | null> {
  const walletIndex = walletSelect.value;
  const wallet = wallets[Number(walletIndex)];
  if (!wallet) {
    throw new Error("Select a Sui wallet.");
  }

  const sameWallet = connectedWalletIndex === walletIndex;
  if (!sameWallet || accounts.length === 0) {
    accounts = await wallet.accounts(config.network || undefined);
    connectedWalletIndex = walletIndex;
    accountSelect.replaceChildren(...accountOptions(accounts));
    accountSelect.disabled = accounts.length <= 1;
  }

  if (accounts.length === 0) {
    throw new Error("Connected wallet did not return an account.");
  }
  if (accounts.length > 1 && accountSelect.value === "") {
    unlockButton.textContent = "Pay with selected address";
    setStatus("Select an account.");
    return null;
  }

  const account = accountByAddress(accountSelect.value) ?? accounts[0];
  if (!account?.address) {
    throw new Error("Select a Sui account.");
  }
  return { wallet, account };
}

function walletOptions(value: SuiWallet[]): HTMLOptionElement[] {
  if (value.length === 0) {
    return [option("", "No wallet detected")];
  }
  return value.map((wallet, index) => option(String(index), wallet.name || `Wallet ${index + 1}`));
}

function accountOptions(value: SuiAccount[]): HTMLOptionElement[] {
  if (value.length === 0) {
    return [option("", "No account")];
  }
  if (value.length === 1) {
    return [option(value[0]?.address ?? "", shortAddress(value[0]?.address ?? ""))];
  }
  return [
    option("", "Select account"),
    ...value.map((account) => option(account.address, shortAddress(account.address)))
  ];
}

function resetAccounts(): void {
  connectedWalletIndex = "";
  accounts = [];
  accountSelect.replaceChildren(option("", "Connect wallet first"));
  accountSelect.disabled = true;
}

function accountByAddress(address: string): SuiAccount | null {
  const wanted = address.trim().toLowerCase();
  return accounts.find((account) => account.address.trim().toLowerCase() === wanted) ?? null;
}

function option(value: string, text: string): HTMLOptionElement {
  const item = document.createElement("option");
  item.value = value;
  item.textContent = text;
  return item;
}

function updatePaymentStatus(event: X402PaymentEvent): void {
  setStatus(event.message);
}

function setBusy(busy: boolean): void {
  refreshButton.disabled = busy;
  walletSelect.disabled = busy || wallets.length === 0;
  accountSelect.disabled = busy || accounts.length <= 1;
  unlockButton.disabled = busy || wallets.length === 0;
}

function setStatus(message: string): void {
  statusEl.textContent = message;
}

function readConfig(): PaymentConfig {
  const node = document.getElementById("payment-config");
  if (!node?.textContent) {
    throw new Error("payment config is missing");
  }
  return JSON.parse(node.textContent) as PaymentConfig;
}

function element<T extends HTMLElement>(id: string): T {
  const node = document.getElementById(id);
  if (!node) {
    throw new Error(`missing element: ${id}`);
  }
  return node as T;
}

function shortAddress(value: string): string {
  const address = value.trim();
  if (address.length <= 18) {
    return address;
  }
  return `${address.slice(0, 10)}...${address.slice(-6)}`;
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim() !== "") {
    return error.message.trim();
  }
  return fallback;
}

async function paymentErrorMessage(response: Response): Promise<string> {
  const body = await response.text();
  if (response.status !== 402) {
    return body.trim() || `Request failed with HTTP ${response.status}.`;
  }

  const parsed = parseJSON<X402PaymentRequiredResponse>(body);
  const requirement = parsed?.accepts?.[0];
  const network = requirement?.network || config.networkName || "selected Sui network";
  const asset = requirement?.extra?.asset || "USDC";
  const amount = formatUSDCAmount(requirement?.amount) || `${config.amount} ${config.asset}`;
  const payTo = requirement?.payTo?.trim().toLowerCase() || "";
  const payer = activePayerAddress.trim().toLowerCase();

  if (payTo !== "" && payer !== "" && payTo === payer) {
    return `Payment settlement failed because the selected wallet is also the recipient. Use a different paying wallet/account, or expose the app with a different --x402-pay-to address. Required amount is ${amount}.`;
  }

  switch ((parsed?.error || "").trim().toLowerCase()) {
    case "payment settlement failed":
      return `Payment settlement failed. The paying wallet likely does not have enough ${network} ${asset}; required amount is ${amount}.`;
    case "payment required":
      return `Payment is required before this content can be unlocked. Required amount is ${amount}.`;
    case "invalid payment payload":
      return "The wallet returned an invalid payment signature. Try reconnecting the wallet and paying again.";
    default:
      return parsed?.error?.trim() || body.trim() || `Payment failed with HTTP ${response.status}.`;
  }
}

function parseJSON<T>(value: string): T | null {
  try {
    return JSON.parse(value) as T;
  } catch {
    return null;
  }
}

function formatUSDCAmount(atomicAmount: string | undefined): string {
  const raw = atomicAmount?.trim();
  if (!raw || !/^\d+$/.test(raw)) {
    return "";
  }
  const padded = raw.padStart(7, "0");
  const whole = padded.slice(0, -6).replace(/^0+(?=\d)/, "");
  const fraction = padded.slice(-6).replace(/0+$/, "");
  return `${fraction ? `${whole}.${fraction}` : whole} USDC`;
}
