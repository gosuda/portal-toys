const peerIdEl = document.getElementById("peerId");
const storageDirEl = document.getElementById("storageDir");
const addressListEl = document.getElementById("addressList");
const serverUrlListEl = document.getElementById("serverUrlList");
const portalStatusEl = document.getElementById("portalStatus");
const binaryStatusEl = document.getElementById("binaryStatus");
const binarySelect = document.getElementById("binarySelect");
const binaryHint = document.getElementById("binaryHint");
const fileTableEl = document.getElementById("fileTable");
const uploadForm = document.getElementById("uploadForm");
const uploadInput = document.getElementById("uploadInput");
const uploadResult = document.getElementById("uploadResult");
const fetchForm = document.getElementById("fetchForm");
const fetchAddress = document.getElementById("fetchAddress");
const fetchFileId = document.getElementById("fetchFileId");
const fetchResult = document.getElementById("fetchResult");
const listRemoteForm = document.getElementById("listRemoteForm");
const listRemoteAddress = document.getElementById("listRemoteAddress");
const remoteListResult = document.getElementById("remoteListResult");
const downloadBinaryBtn = document.getElementById("downloadBinaryBtn");
const launchStatus = document.getElementById("launchStatus");

async function loadInfo() {
  try {
    const res = await fetch("/api/info");
    const data = await res.json();
    peerIdEl.textContent = data.peerId || "-";
    storageDirEl.textContent = data.storageDir || "-";
    renderMonoList(addressListEl, data.addresses || [], "waiting for peers...");
    renderMonoList(serverUrlListEl, data.serverUrls || [], "not configured");
    renderFiles(data.files || []);
    const agentMsg = data.agentRunning
      ? `background agent (pid ${data.agentPid})`
      : "agent not running";
    binaryStatusEl.textContent = `${agentMsg} - binary ${formatBytes(
      data.binarySize || 0
    )}`;
    const serverCount = (data.serverUrls || []).length;
    portalStatusEl.textContent = data.portalActive
      ? `serving via Portal (${serverCount} server URL${serverCount === 1 ? "" : "s"})`
      : "Portal relay disabled";
    renderBinaryOptions(data.binaries || []);
  } catch (err) {
    console.error(err);
  }
}

function renderMonoList(container, items, emptyText) {
  container.innerHTML = "";
  if (!items.length) {
    const li = document.createElement("li");
    li.textContent = emptyText;
    container.appendChild(li);
    return;
  }
  items.forEach((value) => {
    const li = document.createElement("li");
    li.className = "mono";
    li.textContent = value;
    container.appendChild(li);
  });
}

function renderBinaryOptions(binaries) {
  binarySelect.innerHTML = "";
  if (!binaries.length) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No GoReleaser binaries found";
    binarySelect.appendChild(option);
    binarySelect.disabled = true;
    binaryHint.textContent = "Run goreleaser to populate ./dist before downloading.";
    return;
  }
  binarySelect.disabled = false;
  binaries.forEach((bin, index) => {
    const option = document.createElement("option");
    option.value = `${bin.os}|${bin.arch}`;
    option.textContent = `${bin.file} (${formatBytes(bin.size)})`;
    if (index === 0) {
      option.selected = true;
    }
    binarySelect.appendChild(option);
  });
  binaryHint.textContent = "Pick a platform (Windows, macOS, or Linux) - files also live under /dist for direct download.";
}

function renderFiles(files) {
  fileTableEl.innerHTML = "";
  if (!files.length) {
    const row = document.createElement("tr");
    const cell = document.createElement("td");
    cell.colSpan = 5;
    cell.textContent = "No files yet";
    row.appendChild(cell);
    fileTableEl.appendChild(row);
    return;
  }
  files.forEach((file) => {
    const row = document.createElement("tr");
    row.innerHTML = `
      <td class="mono">${file.id}</td>
      <td>${file.name}</td>
      <td>${formatBytes(file.size)}</td>
      <td>${file.source || "local"}</td>
      <td><a href="/download/${file.id}" target="_blank">Download</a></td>
    `;
    fileTableEl.appendChild(row);
  });
}

uploadForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!uploadInput.files.length) {
    uploadResult.textContent = "Select a file first.";
    return;
  }
  const file = uploadInput.files[0];
  const form = new FormData();
  form.append("file", file);
  uploadResult.textContent = "Uploading...";
  try {
    const res = await fetch("/api/upload", {
      method: "POST",
      body: form,
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || "upload failed");
    }
    uploadResult.textContent = `Saved: ${data.file.name}`;
    uploadInput.value = "";
    await loadInfo();
  } catch (err) {
    uploadResult.textContent = err.message;
  }
});

fetchForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  fetchResult.textContent = "Fetching...";
  try {
    const res = await fetch("/api/request", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        multiaddr: fetchAddress.value,
        fileId: fetchFileId.value,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || "fetch failed");
    }
    fetchResult.textContent = `Received: ${data.file.name}`;
    await loadInfo();
  } catch (err) {
    fetchResult.textContent = err.message;
  }
});

listRemoteForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  remoteListResult.textContent = "Listing...";
  try {
    const res = await fetch("/api/list-remote", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ multiaddr: listRemoteAddress.value }),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || "request failed");
    }
    remoteListResult.textContent = JSON.stringify(data.files, null, 2);
  } catch (err) {
    remoteListResult.textContent = err.message;
  }
});

downloadBinaryBtn.addEventListener("click", async () => {
  const selection = binarySelect.value;
  if (!selection) {
    launchStatus.textContent = "No binaries available to download.";
    return;
  }
  const [osName, arch] = selection.split("|");
  downloadBinaryBtn.disabled = true;
  launchStatus.textContent = "Launching background agent...";
  try {
    const res = await fetch("/api/launch", { method: "POST" });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || "launch failed");
    }
    launchStatus.textContent = "Agent started! Download will begin shortly.";
    setTimeout(() => {
      window.location.href = `/binary?os=${encodeURIComponent(
        osName
      )}&arch=${encodeURIComponent(arch)}`;
    }, 300);
    setTimeout(loadInfo, 1500);
  } catch (err) {
    launchStatus.textContent = err.message;
  } finally {
    setTimeout(() => {
      downloadBinaryBtn.disabled = false;
    }, 2000);
  }
});

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const exp = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  );
  const value = bytes / Math.pow(1024, exp);
  return `${value.toFixed(1)} ${units[exp]}`;
}

loadInfo();
