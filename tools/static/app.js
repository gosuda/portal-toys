// Utility helpers
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

// UTF-8 helpers
const enc = new TextEncoder();
const dec = new TextDecoder();

// Hex / Dec codec
function bytesToHex(bytes){
  return Array.from(bytes).map(b=>b.toString(16).padStart(2,'0')).join(' ');
}
function hexToBytes(hex){
  const clean = hex.trim().replace(/[^0-9a-fA-F]/g,' ');
  const parts = clean.split(/\s+/).filter(Boolean);
  return new Uint8Array(parts.map(h=>parseInt(h,16)&255));
}
function bytesToDec(bytes){
  return Array.from(bytes).map(b=>String(b)).join(' ');
}
function decToBytes(decstr){
  const parts = decstr.trim().split(/\s+/).filter(Boolean);
  return new Uint8Array(parts.map(d=>parseInt(d,10)&255));
}

// Base64 helpers (UTF-8 safe)
function b64encode(str){
  const bytes = enc.encode(str);
  let bin = '';
  bytes.forEach(b=>bin += String.fromCharCode(b));
  return btoa(bin);
}
function b64decode(b64){
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for(let i=0;i<bin.length;i++) bytes[i] = bin.charCodeAt(i);
  return dec.decode(bytes);
}

// Wire: Hex/Dec live
const hexText = $('#hexdec-text');
const hexOut = $('#hex-out');
const decOut = $('#dec-out');

hexText.addEventListener('input', () => {
  const text = hexText.value || '';
  const bytes = enc.encode(text);
  hexOut.value = bytesToHex(bytes);
  decOut.value = bytesToDec(bytes);
});

hexOut.addEventListener('input', () => {
  try {
    const bytes = hexToBytes(hexOut.value || '');
    hexText.value = dec.decode(bytes);
    decOut.value = bytesToDec(bytes);
  } catch (e) {
    // ignore invalid input silently
  }
});

decOut.addEventListener('input', () => {
  try {
    const bytes = decToBytes(decOut.value || '');
    hexText.value = dec.decode(bytes);
    hexOut.value = bytesToHex(bytes);
  } catch (e) {
    // ignore invalid input silently
  }
});

// Wire: Base64 live
const b64Text = $('#b64-text');
const b64Out = $('#b64-out');
b64Text.addEventListener('input', () => {
  try { b64Out.value = b64encode(b64Text.value||''); } catch(e) {}
});
b64Out.addEventListener('input', () => {
  try { b64Text.value = b64decode(b64Out.value||''); } catch(e) {}
});

// Wire: JSON live (pretty)
const jsonIn = $('#json-in');
const jsonOut = $('#json-out');
jsonIn.addEventListener('input', () => {
  try { jsonOut.value = JSON.stringify(JSON.parse(jsonIn.value||''), null, 2); } catch (e) { /* ignore */ }
});

// Wire: Case live with mode
const caseIn = $('#case-in');
const caseOut = $('#case-out');
const caseMode = $('#case-mode');
function applyCase() {
  const s = (caseIn.value||'');
  const mode = caseMode.value;
  if (mode === 'lower') caseOut.value = s.toLowerCase();
  else if (mode === 'upper') caseOut.value = s.toUpperCase();
  else {
    const t = s.toLowerCase().replace(/\b\w/g, ch=>ch.toUpperCase());
    caseOut.value = t;
  }
}
caseIn.addEventListener('input', applyCase);
caseMode.addEventListener('change', applyCase);
applyCase();

// Removed QR tool per request

// ==========================
// Unix Timestamp utilities
// ==========================
const tzTargets = [
  { label: 'None', tz: '' },
  { label: 'UTC', tz: 'UTC' },
  { label: 'Asia/Seoul', tz: 'Asia/Seoul' },
  { label: 'America/Los_Angeles', tz: 'America/Los_Angeles' },
  { label: 'America/New_York', tz: 'America/New_York' },
  { label: 'Europe/London', tz: 'Europe/London' },
  { label: 'Europe/Paris', tz: 'Europe/Paris' },
  { label: 'Asia/Tokyo', tz: 'Asia/Tokyo' },
  { label: 'Asia/Shanghai', tz: 'Asia/Shanghai' },
  { label: 'Australia/Sydney', tz: 'Australia/Sydney' },
  { label: 'Asia/Kolkata', tz: 'Asia/Kolkata' },
];

function fmtDate(ms, tz) {
  const d = new Date(ms);
  // yyyy-MM-dd HH:mm:ss
  const f = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  });
  return f.format(d);
}

function updateNow() {
  const now = Date.now();
  $('#now-local').textContent = fmtDate(now, Intl.DateTimeFormat().resolvedOptions().timeZone);
  $('#now-utc').textContent = fmtDate(now, 'UTC');
  $('#now-seconds').textContent = Math.floor(now/1000).toString();
  $('#now-millis').textContent = now.toString();
  updateSelectedTZ(now);
}

function updateSelectedTZ(ms) {
  const select = $('#tz-select');
  if (!select) return;
  const tz = select.value;
  const box = $('#tz-single');
  if (!tz) { box.style.display = 'none'; return; }
  $('#tz-single-label').textContent = tzTargets.find(t=>t.tz===tz)?.label || tz;
  $('#tz-single-time').textContent = fmtDate(ms, tz);
  box.style.display = '';
}

function parseTSInput(s) {
  if (!s) return null;
  const num = Number(s.trim());
  if (!Number.isFinite(num)) return null;
  // Heuristic: >= 1e12 means ms; else seconds
  return num >= 1e12 ? Math.floor(num) : Math.floor(num*1000);
}

// Bindings
// Tool selector (single-view render) with custom tabs
const tools = [
  { id: 'timestamp', name: 'Unix Timestamp', icon: '⏱️' },
  { id: 'hexdec', name: 'Decimal / Hex', icon: '🔢' },
  { id: 'b64', name: 'Base64', icon: '🧬' },
  { id: 'json', name: 'JSON', icon: '🧰' },
  { id: 'case', name: 'Case', icon: '🔤' },
  { id: 'random', name: 'Random', icon: '🎲' },
  { id: 'sha', name: 'SHA', icon: '🔒' },
  { id: 'aes', name: 'AES', icon: '🛡️' },
];
(function initToolTabs(){
  const tabs = $('#tool-tabs');
  const container = $('#tools-container');
  if (!tabs || !container) return;
  tabs.innerHTML = tools.map(t => `<button class="tab" data-id="${t.id}" aria-controls="${t.id}">${t.icon} ${t.name}</button>`).join('');
  function show(id){
    container.querySelectorAll('details.tool').forEach(d => {
      if (d.id === id) { d.style.display = ''; d.setAttribute('open',''); }
      else { d.style.display = 'none'; d.removeAttribute('open'); }
    });
    tabs.querySelectorAll('.tab').forEach(b => b.classList.toggle('active', b.dataset.id === id));
    history.replaceState(null, '', '#' + id);
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }
  tabs.addEventListener('click', (e) => {
    const btn = e.target.closest('.tab');
    if (!btn) return;
    show(btn.dataset.id);
  });
  // default from hash or first
  const initial = location.hash && tools.find(t=>('#'+t.id)===location.hash) ? location.hash.substring(1) : tools[0].id;
  container.querySelectorAll('details.tool').forEach(d => { if (d.id !== initial) { d.style.display = 'none'; d.removeAttribute('open'); } });
  show(initial);
})();

// Build TZ select options
(() => {
  const sel = $('#tz-select');
  if (!sel) return;
  sel.innerHTML = tzTargets.map(t => `<option value="${t.tz}">${t.label}</option>`).join('');
  sel.onchange = () => updateSelectedTZ(Date.now());
})();

$('#ts-now').onclick = updateNow;
setInterval(updateNow, 1000);
updateNow();

// live ts input → time
$('#ts-input').addEventListener('input', () => {
  const ms = parseTSInput($('#ts-input').value);
  if (ms == null) { $('#ts-out-local').textContent = ''; $('#ts-out-utc').textContent = ''; return; }
  $('#ts-out-local').textContent = fmtDate(ms, Intl.DateTimeFormat().resolvedOptions().timeZone);
  $('#ts-out-utc').textContent = fmtDate(ms, 'UTC');
});
$('#ts-clear').onclick = () => {
  $('#ts-input').value = '';
  $('#ts-out-local').textContent = '';
  $('#ts-out-utc').textContent = '';
};

function updateTimeToTs() {
  const val = $('#dt-input').value; // format: YYYY-MM-DDTHH:MM
  if (!val) { $('#dt-out-seconds').textContent = ''; $('#dt-out-millis').textContent = ''; return; }
  const asUTC = $('#dt-as-utc').checked;
  let ms;
  if (asUTC) {
    const [date, time] = val.split('T');
    const [y,m,d] = date.split('-').map(Number);
    const [hh,mm] = (time||'00:00').split(':').map(Number);
    ms = Date.UTC(y, (m||1)-1, d||1, hh||0, mm||0, 0, 0);
  } else {
    ms = new Date(val).getTime();
  }
  $('#dt-out-seconds').textContent = Math.floor(ms/1000).toString();
  $('#dt-out-millis').textContent = Math.floor(ms).toString();
}
$('#dt-input').addEventListener('input', updateTimeToTs);
$('#dt-as-utc').addEventListener('change', updateTimeToTs);

// ==========================
// Random String
// ==========================
const randomLen = $('#random-len');
const randomPreset = $('#random-preset');
const randomCustom = $('#random-custom');
const randomOut = $('#random-out');
$('#random-gen')?.addEventListener('click', () => generateRandom());

function generateRandom(){
  const len = Math.max(1, Math.min(1024, parseInt(randomLen.value||'16',10)));
  let charset = '';
  switch(randomPreset.value){
    case 'alnum': charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'; break;
    case 'letters': charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'; break;
    case 'digits': charset = '0123456789'; break;
    case 'hex': charset = '0123456789abcdef'; break;
    case 'b64': charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'; break;
    case 'custom': charset = (randomCustom.value||'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'); break;
    default: charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  }
  if (charset.length === 0) charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  const bytes = new Uint8Array(len);
  crypto.getRandomValues(bytes);
  let out = '';
  for (let i=0;i<len;i++) out += charset[bytes[i] % charset.length];
  randomOut.value = out;
}
randomLen?.addEventListener('input', generateRandom);
randomPreset?.addEventListener('change', generateRandom);
randomCustom?.addEventListener('input', () => { if (randomPreset.value==='custom') generateRandom(); });
generateRandom();

// ==========================
// SHA Hash
// ==========================
const shaIn = $('#sha-in');
const shaAlgo = $('#sha-algo');
const shaHex = $('#sha-hex');
const shaB64 = $('#sha-b64');

function bytesToHex(buf){ return Array.from(new Uint8Array(buf)).map(b=>b.toString(16).padStart(2,'0')).join(''); }
function bytesToBase64(buf){
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (let i=0;i<bytes.length;i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin);
}

async function updateSHA(){
  const text = shaIn.value||'';
  if (text.length === 0) { shaHex.value=''; shaB64.value=''; return; }
  try{
    const digest = await crypto.subtle.digest(shaAlgo.value, enc.encode(text));
    shaHex.value = bytesToHex(digest);
    shaB64.value = bytesToBase64(digest);
  }catch(e){ shaHex.value=''; shaB64.value=''; }
}
shaIn?.addEventListener('input', updateSHA);
shaAlgo?.addEventListener('change', updateSHA);
updateSHA();

// ==========================
// AES-GCM Encrypt/Decrypt (PBKDF2)
// ==========================
const aesMode = $('#aes-mode');
const aesPass = $('#aes-pass');
const aesPT = $('#aes-pt');
const aesCT = $('#aes-ct');
const aesOut = $('#aes-out');
const aesEncPane = $('#aes-enc-pane');
const aesDecPane = $('#aes-dec-pane');
const aesNew = $('#aes-new');

let currentSalt = null; // Uint8Array(16)
let currentIv = null;   // Uint8Array(12)

function setAesModeUI(){
  const encMode = aesMode.value === 'encrypt';
  aesEncPane.style.display = encMode ? '' : 'none';
  aesDecPane.style.display = encMode ? 'none' : '';
  document.getElementById('aes-tools').style.display = encMode ? '' : 'none';
  aesOut.value = '';
}

function getSalt(){ if (!currentSalt) { currentSalt = new Uint8Array(16); crypto.getRandomValues(currentSalt);} return currentSalt; }
function getIv(){ if (!currentIv) { currentIv = new Uint8Array(12); crypto.getRandomValues(currentIv);} return currentIv; }

async function deriveKey(pass, salt){
  const baseKey = await crypto.subtle.importKey('raw', enc.encode(pass), {name:'PBKDF2'}, false, ['deriveKey']);
  return crypto.subtle.deriveKey(
    {name:'PBKDF2', salt, iterations: 150000, hash: 'SHA-256'},
    baseKey,
    {name:'AES-GCM', length: 256},
    false,
    ['encrypt','decrypt']
  );
}

function concatBytes(a,b,c){
  const len = (a?.length||0)+(b?.length||0)+(c?.length||0);
  const out = new Uint8Array(len);
  let o=0;
  if (a){ out.set(a,o); o+=a.length; }
  if (b){ out.set(b,o); o+=b.length; }
  if (c){ out.set(c,o); }
  return out;
}

function base64ToBytes(b64){
  try{
    const bin = atob(b64.trim());
    const bytes = new Uint8Array(bin.length);
    for (let i=0;i<bin.length;i++) bytes[i] = bin.charCodeAt(i);
    return bytes;
  }catch{ return null; }
}

async function updateAES(){
  const mode = aesMode.value;
  const pass = aesPass.value||'';
  if (!pass) { aesOut.value=''; return; }
  try{
    if (mode === 'encrypt'){
      const pt = aesPT.value||'';
      if (!pt) { aesOut.value=''; return; }
      const salt = getSalt();
      const iv = getIv();
      const key = await deriveKey(pass, salt);
      const ct = await crypto.subtle.encrypt({name:'AES-GCM', iv}, key, enc.encode(pt));
      const packed = concatBytes(salt, iv, new Uint8Array(ct));
      aesOut.value = bytesToBase64(packed);
    } else {
      const data = base64ToBytes(aesCT.value||'');
      if (!data || data.length < 28) { aesOut.value=''; return; }
      const salt = data.slice(0,16);
      const iv = data.slice(16,28);
      const ct = data.slice(28);
      const key = await deriveKey(pass, salt);
      const pt = await crypto.subtle.decrypt({name:'AES-GCM', iv}, key, ct);
      aesOut.value = dec.decode(new Uint8Array(pt));
    }
  }catch(e){
    aesOut.value = '';
  }
}

aesMode?.addEventListener('change', () => { currentSalt=null; currentIv=null; setAesModeUI(); updateAES(); });
aesPass?.addEventListener('input', updateAES);
aesPT?.addEventListener('input', updateAES);
aesCT?.addEventListener('input', updateAES);
aesNew?.addEventListener('click', () => { currentSalt=null; currentIv=null; updateAES(); });
setAesModeUI();
