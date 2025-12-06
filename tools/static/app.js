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
  { id: 'asn1', name: 'ASN.1', icon: '📜' },
  { id: 'diff', name: 'Diff', icon: '🧩' },
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

// ==========================
// Diff Compare (LCS-based)
// ==========================
function tokenize(str, mode){
  if (mode === 'char') return Array.from(str);
  if (mode === 'line') {
    // Keep newlines as tokens to preserve structure
    const out = [];
    const parts = str.split(/(\r?\n)/);
    for (const p of parts) if (p !== '') out.push(p);
    return out;
  }
  // word mode: split but keep whitespace
  const parts = str.split(/(\s+)/);
  return parts.filter(p => p !== '');
}

function lcs(a, b, eq){
  const n = a.length, m = b.length;
  const dp = Array(n+1);
  for (let i=0;i<=n;i++){ dp[i] = new Array(m+1).fill(0); }
  for (let i=1;i<=n;i++){
    for (let j=1;j<=m;j++){
      if (eq(a[i-1], b[j-1])) dp[i][j] = dp[i-1][j-1] + 1;
      else dp[i][j] = dp[i-1][j] >= dp[i][j-1] ? dp[i-1][j] : dp[i][j-1];
    }
  }
  // backtrack
  const script = [];
  let i=n, j=m;
  while (i>0 || j>0){
    if (i>0 && j>0 && eq(a[i-1], b[j-1])) { script.push({t:'eq', v:a[i-1]}); i--; j--; }
    else if (j>0 && (i===0 || dp[i][j-1] >= dp[i-1][j])) { script.push({t:'ins', v:b[j-1]}); j--; }
    else if (i>0) { script.push({t:'del', v:a[i-1]}); i--; }
  }
  script.reverse();
  return script;
}

function escapeHtml(s){
  return s.replace(/[&<>\"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
}

function diffRun(){
  const left = document.getElementById('diff-left').value||'';
  const right = document.getElementById('diff-right').value||'';
  const mode = document.getElementById('diff-mode').value;
  const ignore = document.getElementById('diff-ignore-case').checked;

  const aTok = tokenize(left, mode);
  const bTok = tokenize(right, mode);
  const norm = (x) => ignore ? x.toLowerCase() : x;
  const dp = (function(){
    const n=aTok.length, m=bTok.length; const D=Array(n+1);
    for(let i=0;i<=n;i++){ D[i]=new Array(m+1).fill(0); }
    for(let i=1;i<=n;i++){
      for(let j=1;j<=m;j++){
        if (norm(aTok[i-1]) === norm(bTok[j-1])) D[i][j] = D[i-1][j-1]+1; else D[i][j] = D[i-1][j] >= D[i][j-1] ? D[i-1][j] : D[i][j-1];
      }
    }
    return D;
  })();
  let i=aTok.length, j=bTok.length; const ops=[];
  while(i>0 || j>0){
    if (i>0 && j>0 && norm(aTok[i-1]) === norm(bTok[j-1])) { ops.push({t:'eq', v:aTok[i-1]}); i--; j--; }
    else if (j>0 && (i===0 || dp[i][j-1] >= dp[i-1][j])) { ops.push({t:'ins', v:bTok[j-1]}); j--; }
    else if (i>0) { ops.push({t:'del', v:aTok[i-1]}); i--; }
  }
  ops.reverse();
  // Merge adjacent same-type tokens for readability (except keep whitespace tokens boundaries)
  const merged = [];
  for (const op of ops){
    const last = merged[merged.length-1];
    if (last && last.t===op.t && !(mode!=='line' && (/^\s+$/.test(op.v) || /^\s+$/.test(last.v)))){
      last.v += op.v;
    } else {
      merged.push({t:op.t, v: op.v});
    }
  }
  const html = merged.map(op => `<span class="${op.t}">${escapeHtml(op.v)}</span>`).join('');
  const out = document.getElementById('diff-out');
  out.innerHTML = html;
}

function getEditableText(id){
  const el = document.getElementById(id);
  if (!el) return '';
  // textContent preserves newlines reasonably for contenteditable
  return (el.textContent||'');
}

function setEditableHTML(id, html){
  const el = document.getElementById(id);
  if (el) el.innerHTML = html;
}

function runDiff(){
  const left = getEditableText('diff-left');
  const right = getEditableText('diff-right');
  const mode = document.getElementById('diff-mode').value;
  const ignore = document.getElementById('diff-ignore-case').checked;

  const aTok = tokenize(left, mode);
  const bTok = tokenize(right, mode);
  const norm = (x) => ignore ? x.toLowerCase() : x;
  // DP table
  const dp = (function(){
    const n=aTok.length, m=bTok.length; const D=Array(n+1);
    for(let i=0;i<=n;i++){ D[i]=new Array(m+1).fill(0); }
    for(let i=1;i<=n;i++){
      for(let j=1;j<=m;j++){
        D[i][j] = (norm(aTok[i-1]) === norm(bTok[j-1])) ? (D[i-1][j-1]+1) : (D[i-1][j] >= D[i][j-1] ? D[i-1][j] : D[i][j-1]);
      }
    }
    return D;
  })();
  // Backtrack to ops
  let i=aTok.length, j=bTok.length; const ops=[];
  while(i>0 || j>0){
    if (i>0 && j>0 && norm(aTok[i-1]) === norm(bTok[j-1])) { ops.push({t:'eq', vA:aTok[i-1], vB:bTok[j-1]}); i--; j--; }
    else if (j>0 && (i===0 || dp[i][j-1] >= dp[i-1][j])) { ops.push({t:'ins', vB:bTok[j-1]}); j--; }
    else if (i>0) { ops.push({t:'del', vA:aTok[i-1]}); i--; }
  }
  ops.reverse();

  // Build left and right HTML: highlight deletions on left, insertions on right
  const leftHtml = ops.map(op => {
    if (op.t === 'eq') return escapeHtml(op.vA);
    if (op.t === 'del') return `<span class="diff-red">${escapeHtml(op.vA)}</span>`;
    return ''; // ins not present in left
  }).join('');
  const rightHtml = ops.map(op => {
    if (op.t === 'eq') return escapeHtml(op.vB);
    if (op.t === 'ins') return `<span class="diff-red">${escapeHtml(op.vB)}</span>`;
    return ''; // del not present in right
  }).join('');

  setEditableHTML('diff-left', leftHtml);
  setEditableHTML('diff-right', rightHtml);
}

// Auto-run diff on input/option changes
['diff-left','diff-right'].forEach(id => {
  const el = document.getElementById(id);
  el?.addEventListener('input', () => {
    // Defer to avoid re-entrancy while typing
    requestAnimationFrame(runDiff);
  });
});
document.getElementById('diff-mode')?.addEventListener('change', runDiff);
document.getElementById('diff-ignore-case')?.addEventListener('change', runDiff);
// Initial compare
runDiff();

// ==========================
// ASN.1 (DER) Viewer
// ==========================
function cleanHexToBytes(hex){
  try { return hexToBytes(hex); } catch { return null; }
}
function b64ToBytesSafe(b64){
  try{ return base64ToBytes(b64); }catch{ return null; }
}
function asn1Decode(bytes){
  const nodes = [];
  let off = 0;
  while (off < bytes.length){
    const { node, next } = asn1ReadTLV(bytes, off);
    nodes.push(node);
    off = next;
  }
  return nodes;
}
function asn1ReadTLV(bytes, off){
  if (off >= bytes.length) throw new Error('unexpected end');
  let b = bytes[off++];
  const tagClass = (b >> 6) & 0x03; // 0=univ,1=app,2=ctx,3=priv
  const constructed = !!(b & 0x20);
  let tagNum = b & 0x1f;
  if (tagNum === 0x1f){ // high-tag-number form
    tagNum = 0; let more=true; let count=0;
    while (more){
      if (off >= bytes.length) throw new Error('truncated tag');
      const tb = bytes[off++];
      more = !!(tb & 0x80);
      tagNum = (tagNum << 7) | (tb & 0x7f);
      if (++count > 6) throw new Error('tag too large');
    }
  }
  if (off >= bytes.length) throw new Error('unexpected end len');
  let lenByte = bytes[off++];
  let len;
  if (lenByte === 0x80) throw new Error('indefinite length not supported');
  if (lenByte & 0x80){
    const n = lenByte & 0x7f; if (n === 0 || n > 4) throw new Error('invalid length');
    if (off + n > bytes.length) throw new Error('truncated length');
    len = 0; for (let i=0;i<n;i++) len = (len<<8) | bytes[off++];
  } else { len = lenByte; }
  if (off + len > bytes.length) throw new Error('value truncated');
  const startVal = off; const endVal = off + len;
  let children = null;
  if (constructed){
    children = [];
    let p = startVal;
    while (p < endVal){
      const { node: ch, next } = asn1ReadTLV(bytes, p);
      children.push(ch); p = next;
    }
  }
  const node = { tagClass, constructed, tagNum, len, header: 0 /*unused*/, value: bytes.slice(startVal, endVal), children };
  return { node, next: endVal };
}
function asn1Render(nodes){
  const out = [];
  const clsName = (c)=>(['Universal','Application','Context','Private'][c]||String(c));
  const univName = (t)=>({
    1:'BOOLEAN',2:'INTEGER',3:'BIT STRING',4:'OCTET STRING',5:'NULL',6:'OBJECT IDENTIFIER',
    12:'UTF8String',16:'SEQUENCE',17:'SET',19:'PrintableString',22:'IA5String',23:'UTCTime',24:'GeneralizedTime'
  }[t]||String(t));
  function valPreview(n){
    try{
      if (n.tagClass===0 && !n.constructed){
        switch(n.tagNum){
          case 2: return '0x' + Array.from(n.value).map(b=>b.toString(16).padStart(2,'0')).join('');
          case 5: return 'NULL';
          case 6: return oidToString(n.value);
          case 3: return `bits(${n.value.length}b)`;
          case 4: return 'octets ' + n.value.length;
          case 12: case 19: case 22: return new TextDecoder().decode(n.value);
          case 23: case 24: return new TextDecoder().decode(n.value);
        }
      }
    }catch{}
    return '';
  }
  function oidToString(bytes){ if (!bytes || bytes.length===0) return ''; const a = []; const b0 = bytes[0]; a.push(Math.floor(b0/40)); a.push(b0%40); let v=0; for(let i=1;i<bytes.length;i++){ const b=bytes[i]; v=(v<<7)|(b&0x7f); if(!(b&0x80)){ a.push(v); v=0; } } return a.join('.'); }
  function renderNode(n, depth){
    const indent = '<span class="asn1-indent">'.repeat(depth) + '</span>';
    const tagText = (n.tagClass===0?univName(n.tagNum):String(n.tagNum));
    const pv = valPreview(n);
    out.push(`${indent}<span class="asn1-node"><span class="asn1-class">${clsName(n.tagClass)}</span> <span class="asn1-tag">${tagText}${n.constructed?' (C)':''}</span> <span class="asn1-len">len=${n.len}</span>${pv? ' <span class="asn1-val">'+escapeHtml(pv)+'</span>':''}</span>`);
    if (n.children){ for(const ch of n.children) renderNode(ch, depth+1); }
  }
  for (const n of nodes) renderNode(n, 0);
  return out.join('\n');
}
function runASN1(){
  const input = document.getElementById('asn1-in');
  const fmt = document.getElementById('asn1-format').value;
  const err = document.getElementById('asn1-err');
  const out = document.getElementById('asn1-out');
  err.textContent = ''; out.textContent = '';
  const txt = (input.value||'').trim(); if (!txt){ return; }
  let bytes = null;
  if (fmt === 'hex') bytes = cleanHexToBytes(txt);
  else if (fmt === 'b64') bytes = b64ToBytesSafe(txt);
  else {
    // auto: try base64 first, then hex
    bytes = b64ToBytesSafe(txt) || cleanHexToBytes(txt);
  }
  if (!bytes){ err.textContent = 'Failed to parse input as hex or base64.'; return; }
  try{
    const nodes = asn1Decode(bytes);
    out.innerHTML = asn1Render(nodes);
  }catch(e){ err.textContent = 'ASN.1 parse error: ' + (e?.message||String(e)); }
}
document.getElementById('asn1-in')?.addEventListener('input', () => requestAnimationFrame(runASN1));
document.getElementById('asn1-format')?.addEventListener('change', runASN1);
