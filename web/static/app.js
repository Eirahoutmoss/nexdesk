// NexDesk web technician app. Speaks the same protocol as the Windows and
// Android apps: JSON signaling over WebSocket (v3, multi-technician), one
// WebRTC peer with "ctl" / "file" / "screen" data channels, chunked screen
// frames with 64x64 delta tiles, INPUT:{json} control messages.
'use strict';

const VER = '1.5.0';
const $ = (s) => document.querySelector(s);
const enc = new TextEncoder();
const dec = new TextDecoder();

// ------------------------------------------------------------------ state
const S = {
  ws: null, pc: null, ctl: null, scr: null,
  code: '', title: '', device: null,
  turn: null, announced: false, stopped: true, reconnecting: false,
  discTimer: 0, frames: 0, bytes: 0, statAt: 0, statF: 0, statB: 0,
  monitors: 0, curMon: 0, roster: '', inputBy: '', chat: [], unread: false,
  fb: document.createElement('canvas'), fbReady: false, keyReqAt: 0,
  zoom: 1, pan: { x: 0, y: 0 }, lastPt: { x: 0.5, y: 0.5 }, wake: null,
};
let frameChain = Promise.resolve();

// ------------------------------------------------------------------ storage
const store = {
  get(k, d) { try { const v = localStorage.getItem('nd.' + k); return v === null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem('nd.' + k, JSON.stringify(v)); } catch { /* private mode */ } },
};

// Device passwords are encrypted with a non-extractable AES key kept in
// IndexedDB: the page can use it, script cannot read the key itself out.
function idb() {
  return new Promise((res, rej) => {
    const r = indexedDB.open('nexdesk', 1);
    r.onupgradeneeded = () => r.result.createObjectStore('keys');
    r.onsuccess = () => res(r.result);
    r.onerror = () => rej(r.error);
  });
}
async function secretKey() {
  const db = await idb();
  const get = () => new Promise((res) => {
    const q = db.transaction('keys').objectStore('keys').get('pw');
    q.onsuccess = () => res(q.result || null); q.onerror = () => res(null);
  });
  let k = await get();
  if (!k) {
    k = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
    await new Promise((res, rej) => {
      const t = db.transaction('keys', 'readwrite'); t.objectStore('keys').put(k, 'pw');
      t.oncomplete = res; t.onerror = () => rej(t.error);
    });
  }
  return k;
}
const b64 = (u8) => btoa(String.fromCharCode(...u8));
const unb64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
async function seal(text) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await secretKey(), enc.encode(text)));
  return b64(iv) + '.' + b64(ct);
}
async function unseal(s) {
  const [iv, ct] = s.split('.');
  return dec.decode(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: unb64(iv) }, await secretKey(), unb64(ct)));
}

// ------------------------------------------------------------------ helpers
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const normCode = (s) => { const c = String(s || '').replace(/[\s\-. ]/g, ''); return /^\d{6,14}$/.test(c) ? c : ''; };
const show = (el, on) => el.classList.toggle('hidden', !on);
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function wsURL() {
  const custom = store.get('server', '').trim();
  if (custom) return custom.split('#')[0];
  return (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/v1/ws';
}

function errorText(reason) {
  const r = String(reason || '');
  if (r.includes('invalid_or_expired_code')) return 'Kod bulunamadı ya da süresi doldu. Karşı tarafta yeni kod oluşturun. (Cihazsa: çevrimdışı olabilir.)';
  if (r.includes('device_auth_failed')) return 'Cihaz kimliği ya da parolası hatalı.';
  if (r.includes('device_busy')) return 'Cihaz başka bir oturumda (karşı tarafta eski NexDesk sürümü).';
  if (r.includes('session_full')) return 'Oturumdaki teknisyen sayısı üst sınırda.';
  if (r.includes('rate_limited')) return 'Çok fazla hatalı deneme — 1 dakika bekleyin.';
  if (r.includes('client_outdated')) return 'Uygulama bu sunucu için eski; sayfayı yenileyin.';
  return 'Bağlanılamadı: ' + r;
}

// ------------------------------------------------------------------ dialogs
function dialog(title, bodyHTML, buttons) {
  const d = $('#dlg');
  $('#dlgTitle').textContent = title;
  $('#dlgBody').innerHTML = bodyHTML;
  const acts = $('#dlgActions');
  acts.innerHTML = '';
  return new Promise((resolve) => {
    for (const [label, val, cls] of buttons) {
      const b = document.createElement('button');
      b.type = 'button'; b.textContent = label; if (cls) b.className = cls;
      b.onclick = () => { d.close(); resolve(val); };
      acts.appendChild(b);
    }
    d.onclose = () => resolve(null);
    d.showModal();
  });
}

function notice(text) {
  const n = $('#notice');
  n.textContent = text || '';
  show(n, !!text);
}

// ------------------------------------------------------------------ home
async function renderDevices() {
  const list = $('#devices');
  const devs = store.get('devices', []);
  list.innerHTML = '';
  show($('#noDevices'), devs.length === 0);
  for (const d of devs) {
    const li = document.createElement('li');
    li.innerHTML = `<span>🖥</span><div class="grow"><div>${esc(d.label || d.id)}</div>${d.label ? `<div class="sub">${esc(d.id)}</div>` : ''}</div><button class="icon" title="Sil" aria-label="Sil">🗑</button>`;
    li.querySelector('.grow').onclick = async () => {
      if (!needName()) return;
      let pass = '';
      try { pass = await unseal(d.pass); } catch { /* key lost */ }
      if (!pass) { notice('Bu cihazın parolası okunamadı; cihazı silip yeniden ekleyin.'); return; }
      connect({ device: d.id, pass, title: d.label || d.id });
    };
    li.querySelector('button').onclick = () => {
      store.set('devices', store.get('devices', []).filter((x) => x.id !== d.id));
      renderDevices();
    };
    list.appendChild(li);
  }
}

async function addDevice() {
  const v = await dialog('Cihaz ekle',
    `<input id="dId" placeholder="Cihaz kimliği" autocomplete="off">
     <input id="dLabel" placeholder="Ad (ör. HBYS Sunucu)" autocomplete="off">
     <input id="dPass" type="password" placeholder="Parola" autocomplete="new-password">`,
    [['Vazgeç', null], ['Kaydet', 'save'], ['Kaydet ve bağlan', 'go', 'primary']]);
  if (!v) return;
  const id = $('#dId').value.trim(), label = $('#dLabel').value.trim(), pass = $('#dPass').value;
  if (!id || pass.length < 4) { notice('Cihaz kimliği ve en az 4 karakterlik parola gerekli.'); return; }
  let sealed = '';
  try { sealed = await seal(pass); } catch { notice('Bu tarayıcı parolayı güvenli saklayamıyor (gizli pencere?); cihaz kaydedilmedi.'); }
  if (sealed) {
    const devs = store.get('devices', []).filter((x) => x.id.toLowerCase() !== id.toLowerCase());
    devs.push({ id, label, pass: sealed });
    devs.sort((a, b) => (a.label || a.id).localeCompare(b.label || b.id, 'tr'));
    store.set('devices', devs);
    renderDevices();
  }
  if (v === 'go' && needName()) connect({ device: id, pass, title: label || id });
}

async function settings() {
  const v = await dialog('Ayarlar',
    `<label for="sSrv">Sunucu <span class="muted small">(boş: bu sayfanın sunucusu)</span></label>
     <input id="sSrv" placeholder="wss://…/v1/ws" value="${esc(store.get('server', ''))}">
     <p class="muted small">Sürüm ${VER}</p>`,
    [['Vazgeç', null], ['Kaydet', 'ok', 'primary']]);
  if (v === 'ok') store.set('server', $('#sSrv').value.trim());
}

function needName() {
  const n = $('#name').value.trim();
  if (!n) { notice('Önce adınızı girin — karşı taraf kimin bağlandığını görür.'); $('#name').focus(); return false; }
  store.set('name', n);
  return true;
}

// ------------------------------------------------------------------ session
function status(t) { $('#ovText').textContent = t; S.status = t; updateLine(); }
function updateLine() {
  const l = S.inputBy ? 'Kontrol: ' + S.inputBy : S.roster ? 'Oturumda: ' + S.roster : (S.status || '');
  $('#sLine').textContent = l;
}

function openSession(title) {
  S.title = title;
  $('#sTitle').textContent = title;
  show($('#home'), false); show($('#session'), true);
  show($('#overlay'), true); show($('#tMon'), false); show($('#keybar'), false);
  S.fbReady = false; S.zoom = 1; S.pan = { x: 0, y: 0 };
  resizeView(); draw();
  try { navigator.wakeLock?.request('screen').then((w) => { S.wake = w; }).catch(() => {}); } catch { /* unsupported */ }
}

function end(reason) {
  S.stopped = true;
  clearTimeout(S.discTimer);
  try { S.pc?.close(); } catch {}
  try { S.ws?.close(); } catch {}
  S.pc = S.ctl = S.scr = S.ws = null;
  try { S.wake?.release(); } catch {}
  S.wake = null;
  $('#kbd').blur();
  show($('#session'), false); show($('#home'), true);
  notice(reason || '');
}

async function connect({ code, device, pass, title }) {
  notice('');
  Object.assign(S, {
    stopped: false, announced: false, reconnecting: false, turn: null,
    code: code || device, device: device || null, frames: 0, bytes: 0, statAt: 0,
    monitors: 0, curMon: 0, roster: '', inputBy: '', chat: [], unread: false, status: '',
  });
  openSession(title || code);
  status('Sunucuya bağlanılıyor…');
  let ws;
  try { ws = new WebSocket(wsURL()); } catch (e) { end('Sunucu adresi geçersiz.'); return; }
  S.ws = ws;
  const name = store.get('name', '');
  ws.onopen = () => {
    const m = { type: 'join', v: 3, app: 'web/' + VER, name };
    if (device) { m.device_id = device; m.pass = pass; } else { m.code = code; }
    ws.send(JSON.stringify(m));
  };
  let joined = false;
  ws.onmessage = async (ev) => {
    if (ws !== S.ws) return;
    let m; try { m = JSON.parse(ev.data); } catch { return; }
    switch (m.type) {
      case 'joined':
        joined = true;
        status(device ? 'Cihaza bağlanılıyor…' : 'Karşı tarafın onayı bekleniyor…');
        break;
      case 'approved':
        if (m.ice) S.turn = m.ice;
        try { await negotiate(); status('Güvenli bağlantı kuruluyor…'); } catch (e) { end('WebRTC başlatılamadı: ' + e.message); }
        break;
      case 'signal':
        if (m.kind === 'answer' && S.pc && m.payload) {
          try { await S.pc.setRemoteDescription(m.payload); } catch (e) { console.warn('answer', e); }
        } else if (m.kind === 'candidate' && S.pc && m.payload) {
          try { await S.pc.addIceCandidate(m.payload); } catch { /* non-trickle peers send none */ }
        }
        break;
      case 'rejected': end('Karşı taraf isteği reddetti.'); break;
      case 'closed': end(m.reason === 'kicked' ? 'Karşı taraf bağlantınızı kesti.' : 'Oturum sonlandırıldı.'); break;
      case 'error':
        if (!joined) end(errorText(m.reason));
        break;
    }
  };
  ws.onclose = () => {
    if (ws !== S.ws || S.stopped) return;
    if (S.announced && S.pc && ['connected', 'disconnected'].includes(S.pc.connectionState)) {
      status('Sunucu bağlantısı koptu; oturum sürüyor.');
      return; // media is peer-to-peer; the session lives until the peer drops
    }
    end(joined ? 'Bağlantı kapandı.' : 'Sunucuya ulaşılamadı.');
  };
}

function iceServers() {
  const list = [{ urls: ['stun:stun.l.google.com:19302', 'stun:stun1.l.google.com:19302'] }];
  if (S.turn && S.turn.urls && S.turn.urls.length) {
    list.push({ urls: S.turn.urls, username: S.turn.username, credential: S.turn.credential });
  }
  return list;
}

// negotiate builds a fresh peer and sends a complete (non-trickle) offer.
async function negotiate() {
  const pc = new RTCPeerConnection({ iceServers: iceServers() });
  const ctl = pc.createDataChannel('ctl');
  pc.createDataChannel('file');
  const scr = pc.createDataChannel('screen');
  scr.binaryType = 'arraybuffer';
  ctl.onmessage = (e) => { if (typeof e.data === 'string') onControl(e.data); };
  ctl.onopen = () => {
    if (pc !== S.pc) return;
    sendCtl('HELLO:' + (store.get('name', '') || 'Web') + ' (Tarayıcı)');
  };
  scr.onmessage = (e) => { if (pc === S.pc && e.data instanceof ArrayBuffer) onScreenChunk(new Uint8Array(e.data)); };
  const old = S.pc;
  S.pc = pc; S.ctl = ctl; S.scr = scr; S.chunk = null;
  if (old) try { old.close(); } catch {}
  pc.onconnectionstatechange = () => onPeerState(pc);
  pc.oniceconnectionstatechange = () => { if (!('connectionState' in pc)) onPeerState(pc); };
  await pc.setLocalDescription(await pc.createOffer());
  await waitGathering(pc, 10000);
  const d = pc.localDescription;
  S.ws.send(JSON.stringify({ type: 'signal', kind: 'offer', payload: { type: d.type, sdp: d.sdp } }));
}

function waitGathering(pc, ms) {
  if (pc.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise((res) => {
    const t = setTimeout(res, ms); // send what we have after ms
    pc.addEventListener('icegatheringstatechange', () => { if (pc.iceGatheringState === 'complete') { clearTimeout(t); res(); } });
  });
}

function peerState(pc) { return pc.connectionState || ({ completed: 'connected', checking: 'connecting' }[pc.iceConnectionState] || pc.iceConnectionState); }

function onPeerState(pc) {
  if (pc !== S.pc || S.stopped) return;
  const st = peerState(pc);
  if (st === 'connected') {
    clearTimeout(S.discTimer); S.discTimer = 0;
    if (!S.announced) { S.announced = true; onConnected(); } else status('Bağlantı toparlandı.');
  } else if (st === 'disconnected') {
    if (S.discTimer) return;
    if (S.announced) status('Bağlantı zayıfladı — toparlanması bekleniyor…');
    S.discTimer = setTimeout(() => { S.discTimer = 0; if (pc === S.pc && peerState(pc) !== 'connected') peerFailed(); }, 6000);
  } else if (st === 'failed' || st === 'closed') {
    peerFailed();
  }
}

function peerFailed() {
  if (S.stopped) return;
  if (!S.announced) { end('Güvenli bağlantı kurulamadı (ağ engeli olabilir).'); return; }
  reconnect();
}

async function reconnect() {
  if (S.reconnecting) return;
  S.reconnecting = true;
  show($('#overlay'), true);
  try {
    for (let i = 1; i <= 6; i++) {
      if (S.stopped || !S.ws || S.ws.readyState !== 1) break;
      status(`Yeniden bağlanılıyor… (${i}/6)`);
      await sleep(i * 1000);
      try { await negotiate(); } catch { continue; }
      for (let t = 0; t < 33; t++) {
        await sleep(300);
        if (S.pc && peerState(S.pc) === 'connected') { show($('#overlay'), false); return; }
      }
    }
    if (!S.stopped) end('Yeniden bağlanılamadı.');
  } finally { S.reconnecting = false; }
}

async function onConnected() {
  show($('#overlay'), false);
  status('Bağlandı');
  focusKbd();
}

function sendCtl(t) {
  if (S.ctl && S.ctl.readyState === 'open') { try { S.ctl.send(t); return true; } catch {} }
  return false;
}

function onControl(raw) {
  if (raw.startsWith('ROSTER:')) {
    try {
      const me = store.get('name', '');
      S.roster = JSON.parse(raw.slice(7)).map((e) => e.name).filter((n) => n && n !== me).join(', ');
      updateLine();
    } catch {}
  } else if (raw.startsWith('INPUTBY:')) {
    const who = raw.slice(8);
    S.inputBy = who === store.get('name', '') ? '' : who;
    updateLine();
  } else if (raw.startsWith('CHAT:')) {
    S.chat.push('Karşı taraf: ' + raw.slice(5));
    S.unread = true; $('#tChat').textContent = '💬•';
    if (chatOpen) renderChat();
  } else if (raw.startsWith('MONITORS:')) {
    try { S.monitors = JSON.parse(raw.slice(9)).length; } catch { S.monitors = 0; }
    show($('#tMon'), S.monitors > 1); updateMon();
  } else if (raw === 'PING') {
    sendCtl('PONG');
  } else if (raw.startsWith('SECURE_DESKTOP:')) {
    status(raw.endsWith('1') ? 'Karşı tarafta güvenli masaüstü açık (Ctrl+Alt+Del / UAC / kilit).' : 'Bağlandı');
  }
}

function updateMon() { $('#tMonTxt').textContent = S.monitors > 1 ? ` ${S.curMon + 1}/${S.monitors}` : ''; }

// ------------------------------------------------------------------ screen frames
// Chunk: "RSC1" | u64 seq | u16 idx | u16 count | u32 total | payload
// Frame: "RSF1" | u8 ver | u16 monitor | u64 seq | u32 w | u32 h | u32 len | JPEG
function onScreenChunk(m) {
  if (m.length < 20 || m[0] !== 82 || m[1] !== 83 || m[2] !== 67 || m[3] !== 49) return;
  const dv = new DataView(m.buffer, m.byteOffset, m.byteLength);
  const seq = dv.getBigUint64(4), idx = dv.getUint16(12), count = dv.getUint16(14), total = dv.getUint32(16);
  const payload = m.subarray(20);
  if (!count || idx >= count || !total || !payload.length) return;
  let c = S.chunk;
  if (!c || c.seq !== seq || (idx === 0 && c.next !== 0)) c = S.chunk = { seq, count, total, buf: new Uint8Array(total), len: 0, next: 0 };
  if (c.count !== count || c.total !== total || idx !== c.next || c.len + payload.length > total) { S.chunk = null; return; }
  c.buf.set(payload, c.len); c.len += payload.length; c.next++;
  if (c.next !== count) return;
  S.chunk = null;
  if (c.len !== total) return;
  const f = decodeFrame(c.buf);
  if (!f) return;
  S.frames++; S.bytes += f.jpeg.length;
  frameChain = frameChain.then(() => applyFrame(f)).catch(() => {});
}

function decodeFrame(w) {
  if (w.length < 27 || w[0] !== 82 || w[1] !== 83 || w[2] !== 70 || w[3] !== 49 || w[4] !== 1) return null;
  const dv = new DataView(w.buffer, w.byteOffset, w.byteLength);
  const len = dv.getUint32(23);
  if (len !== w.length - 27) return null;
  return { monitor: dv.getUint16(5), width: dv.getUint32(15), height: dv.getUint32(19), jpeg: w.subarray(27) };
}

// Tile segment right after SOI: FF FE len "NXT1" u16 size u16 cols u16 n n×(u16 tx,u16 ty)
function parseTiles(j) {
  if (j.length < 14 || j[2] !== 0xff || j[3] !== 0xfe) return null;
  const dv = new DataView(j.buffer, j.byteOffset, j.byteLength);
  const l = dv.getUint16(4);
  if (l < 12 || 4 + l > j.length || dec.decode(j.subarray(6, 10)) !== 'NXT1') return null;
  const size = dv.getUint16(10), cols = dv.getUint16(12), n = dv.getUint16(14);
  if (!size || !cols || l !== 2 + 10 + 4 * n) return null;
  const tiles = [];
  for (let i = 0; i < n; i++) tiles.push([dv.getUint16(16 + 4 * i), dv.getUint16(18 + 4 * i)]);
  return { size, cols, tiles };
}

async function applyFrame(f) {
  const meta = parseTiles(f.jpeg);
  const fb = S.fb;
  if (meta && (!S.fbReady || fb.width !== f.width || fb.height !== f.height)) {
    const now = Date.now();
    if (now - S.keyReqAt > 1000) { S.keyReqAt = now; sendCtl('KEYFRAME'); }
    return;
  }
  const bmp = await createImageBitmap(new Blob([f.jpeg], { type: 'image/jpeg' }));
  const ctx = fb.getContext('2d');
  if (!meta) {
    if (fb.width !== f.width || fb.height !== f.height) { fb.width = f.width; fb.height = f.height; }
    ctx.drawImage(bmp, 0, 0, f.width, f.height);
    S.fbReady = true;
  } else {
    meta.tiles.forEach(([tx, ty], i) => {
      const ax = (i % meta.cols) * meta.size, ay = Math.floor(i / meta.cols) * meta.size;
      const x0 = tx * meta.size, y0 = ty * meta.size;
      const tw = Math.min(meta.size, f.width - x0), th = Math.min(meta.size, f.height - y0);
      if (tw > 0 && th > 0 && ax + tw <= bmp.width && ay + th <= bmp.height) ctx.drawImage(bmp, ax, ay, tw, th, x0, y0, tw, th);
    });
  }
  bmp.close?.();
  if (S.curMon !== f.monitor) { S.curMon = f.monitor; updateMon(); }
  draw();
}

// ------------------------------------------------------------------ view
const view = $('#view');
let drawQueued = false;
function resizeView() {
  const r = view.getBoundingClientRect(), dpr = window.devicePixelRatio || 1;
  view.width = Math.max(1, Math.round(r.width * dpr));
  view.height = Math.max(1, Math.round(r.height * dpr));
  draw();
}
function geometry() {
  if (!S.fbReady) return null;
  const vw = view.width, vh = view.height, fw = S.fb.width, fh = S.fb.height;
  const fit = Math.min(vw / fw, vh / fh), s = fit * S.zoom, dpr = window.devicePixelRatio || 1;
  const w = fw * s, h = fh * s;
  return { x: (vw - w) / 2 + S.pan.x * dpr, y: (vh - h) / 2 + S.pan.y * dpr, w, h, dpr };
}
function draw() {
  if (drawQueued) return;
  drawQueued = true;
  requestAnimationFrame(() => {
    drawQueued = false;
    const ctx = view.getContext('2d');
    ctx.fillStyle = '#000'; ctx.fillRect(0, 0, view.width, view.height);
    const g = geometry();
    if (!g) return;
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(S.fb, g.x, g.y, g.w, g.h);
  });
}
function toRemote(clientX, clientY) {
  const g = geometry();
  if (!g) return null;
  const r = view.getBoundingClientRect();
  const px = (clientX - r.left) * g.dpr, py = (clientY - r.top) * g.dpr;
  const x = (px - g.x) / g.w, y = (py - g.y) / g.h;
  if (x < 0 || x > 1 || y < 0 || y > 1) return null;
  S.lastPt = { x, y };
  return { x, y };
}

// ------------------------------------------------------------------ input
function input(ev) { sendCtl('INPUT:' + JSON.stringify(ev)); }
function mouse(t, p, b = 0, d = 0) {
  if (!p) return;
  const ev = { t, x: +p.x.toFixed(5), y: +p.y.toFixed(5) };
  if (b) ev.b = b; if (d) ev.d = d;
  input(ev);
}
function click(p, b) { if (!p) return; mouse('move', p); mouse('down', p, b); mouse('up', p, b); }
function tap(vk) { input({ t: 'keydown', k: vk }); input({ t: 'keyup', k: vk }); }
function combo(vks) { vks.forEach((k) => input({ t: 'keydown', k })); [...vks].reverse().forEach((k) => input({ t: 'keyup', k })); }
function text(s) { for (let i = 0; i < s.length; i += 200) input({ t: 'text', s: s.slice(i, i + 200) }); }

// Pointer: mouse = real mouse; touch = tap/long-press/drag, two fingers zoom/pan.
const ptrs = new Map();
let g = null, moveQueued = null;
view.addEventListener('contextmenu', (e) => e.preventDefault());
view.addEventListener('pointerdown', (e) => {
  e.preventDefault();
  view.setPointerCapture(e.pointerId);
  if (e.pointerType === 'mouse') {
    const p = toRemote(e.clientX, e.clientY);
    mouse('move', p); mouse('down', p, [0, 1, 2][e.button] ?? 0);
    return;
  }
  ptrs.set(e.pointerId, { x: e.clientX, y: e.clientY });
  if (ptrs.size === 1) {
    g = { start: { x: e.clientX, y: e.clientY }, last: { x: e.clientX, y: e.clientY }, mode: 0, long: false, prev: null };
    g.timer = setTimeout(() => { if (g && g.mode === 0) { g.long = true; click(toRemote(g.start.x, g.start.y), 2); } }, 500);
  } else if (g) {
    if (g.mode === 1) mouse('up', toRemote(g.last.x, g.last.y));
    g.mode = 2; clearTimeout(g.timer); g.prev = null;
  }
});
view.addEventListener('pointermove', (e) => {
  if (e.pointerType === 'mouse') {
    if (!moveQueued) requestAnimationFrame(() => { const q = moveQueued; moveQueued = null; mouse('move', toRemote(q.x, q.y)); });
    moveQueued = { x: e.clientX, y: e.clientY };
    return;
  }
  if (!ptrs.has(e.pointerId) || !g) return;
  ptrs.set(e.pointerId, { x: e.clientX, y: e.clientY });
  if (ptrs.size >= 2) {
    const pts = [...ptrs.values()];
    const c = { x: pts.reduce((a, p) => a + p.x, 0) / pts.length, y: pts.reduce((a, p) => a + p.y, 0) / pts.length };
    const span = pts.reduce((a, p) => a + Math.hypot(p.x - c.x, p.y - c.y), 0) / pts.length;
    if (g.prev && g.prev.n === pts.length && g.prev.span > 0) {
      S.zoom = Math.min(6, Math.max(1, S.zoom * span / g.prev.span));
      S.pan = S.zoom <= 1.001 ? { x: 0, y: 0 } : { x: S.pan.x + c.x - g.prev.c.x, y: S.pan.y + c.y - g.prev.c.y };
      draw();
    }
    g.prev = { c, span, n: pts.length };
    return;
  }
  if (g.mode === 2 || g.long) return;
  const p = { x: e.clientX, y: e.clientY };
  if (g.mode === 0 && Math.hypot(p.x - g.start.x, p.y - g.start.y) > 8) {
    g.mode = 1; clearTimeout(g.timer);
    const s = toRemote(g.start.x, g.start.y);
    mouse('move', s); mouse('down', s, 0);
  }
  if (g.mode === 1) { g.last = p; mouse('move', toRemote(p.x, p.y)); }
});
function pointerEnd(e) {
  if (e.pointerType === 'mouse') {
    if (e.type === 'pointerup') mouse('up', toRemote(e.clientX, e.clientY), [0, 1, 2][e.button] ?? 0);
    focusKbd();
    return;
  }
  if (!ptrs.delete(e.pointerId) || !g) return;
  if (ptrs.size === 0) {
    clearTimeout(g.timer);
    if (g.mode === 0 && !g.long && e.type === 'pointerup') click(toRemote(g.start.x, g.start.y), 0);
    if (g.mode === 1) mouse('up', toRemote(g.last.x, g.last.y));
    g = null;
  } else if (g.mode === 2) {
    g.prev = null;
  }
}
view.addEventListener('pointerup', pointerEnd);
view.addEventListener('pointercancel', pointerEnd);
let wheelAcc = 0;
view.addEventListener('wheel', (e) => {
  e.preventDefault();
  wheelAcc += e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY;
  const p = toRemote(e.clientX, e.clientY) || S.lastPt;
  while (Math.abs(wheelAcc) >= 100) {
    const up = wheelAcc < 0;
    mouse('wheel', p, 0, up ? 120 : -120);
    wheelAcc += up ? 100 : -100;
  }
}, { passive: false });

// Keyboard (desktop browsers): printable characters go as Unicode text, so the
// remote layout does not matter; everything else as Windows virtual keys.
const VK = {
  Backspace: 8, Tab: 9, Enter: 13, NumpadEnter: 13, ShiftLeft: 16, ShiftRight: 16, ControlLeft: 17, ControlRight: 17,
  AltLeft: 18, AltRight: 18, Pause: 19, CapsLock: 20, Escape: 27, Space: 32, PageUp: 33, PageDown: 34, End: 35, Home: 36,
  ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40, PrintScreen: 44, Insert: 45, Delete: 46,
  MetaLeft: 91, MetaRight: 92, ContextMenu: 93, NumpadMultiply: 106, NumpadAdd: 107, NumpadSubtract: 109,
  NumpadDecimal: 110, NumpadDivide: 111, NumLock: 144, ScrollLock: 145,
};
function vkOf(code) {
  if (VK[code]) return VK[code];
  let m;
  if ((m = /^Key([A-Z])$/.exec(code))) return m[1].charCodeAt(0);
  if ((m = /^Digit(\d)$/.exec(code))) return 48 + +m[1];
  if ((m = /^Numpad(\d)$/.exec(code))) return 96 + +m[1];
  if ((m = /^F(\d{1,2})$/.exec(code)) && +m[1] >= 1 && +m[1] <= 24) return 111 + +m[1];
  return 0;
}
const held = new Map();
function sessionActive() { return !$('#session').classList.contains('hidden') && !$('#dlg').open; }
const VIA_INPUT = ['Backspace', 'Enter', 'Unidentified', 'Process', 'Dead'];
// keyDown: printable characters arrive as text (from the focused hidden
// textarea's input event, or here when focus is elsewhere); other keys and
// shortcuts go as virtual keys that stay down until their keyup, so
// Ctrl/Shift/Alt combinations work.
function keyDown(e, fromKbd) {
  const mods = e.ctrlKey || e.metaKey || e.altKey;
  const altGr = e.getModifierState && e.getModifierState('AltGraph');
  if (e.key && e.key.length === 1 && (altGr || !mods)) {
    if (fromKbd) return; // the input event delivers it (IME/composition safe)
    e.preventDefault(); text(e.key); return;
  }
  if (fromKbd && !mods && VIA_INPUT.includes(e.key)) return;
  const vk = vkOf(e.code);
  if (!vk) return;
  e.preventDefault();
  held.set(e.code, vk);
  input({ t: 'keydown', k: vk });
}
function keyUp(e) {
  const vk = held.get(e.code);
  if (vk === undefined) return;
  held.delete(e.code);
  e.preventDefault();
  input({ t: 'keyup', k: vk });
}
window.addEventListener('keydown', (e) => { if (sessionActive() && e.target !== $('#kbd')) keyDown(e, false); });
window.addEventListener('keyup', (e) => { if (e.target !== $('#kbd')) keyUp(e); });
window.addEventListener('blur', () => { for (const vk of held.values()) input({ t: 'keyup', k: vk }); held.clear(); });

// Phone keyboards: a hidden textarea; changes are diffed and sent as text /
// backspaces (the sentinel lets Backspace be detected on an "empty" field).
const SENT = '  ';
const kbd = $('#kbd');
kbd.value = SENT;
let kbdOld = SENT;
kbd.addEventListener('input', (e) => {
  const nv = kbd.value;
  let p = 0;
  while (p < kbdOld.length && p < nv.length && kbdOld[p] === nv[p]) p++;
  for (let i = 0; i < kbdOld.length - p; i++) tap(8);
  const ins = nv.slice(p);
  if (ins) text(ins);
  if (nv.length < SENT.length || (!e.isComposing && nv.length > 64)) { kbd.value = SENT; kbd.setSelectionRange(SENT.length, SENT.length); }
  kbdOld = kbd.value;
});
kbd.addEventListener('keydown', (e) => { if (sessionActive()) keyDown(e, true); });
kbd.addEventListener('keyup', keyUp);
// With a physical keyboard (no touch), keep the hidden field focused so typed
// text arrives through it — this also covers dead keys and IMEs.
const finePointer = window.matchMedia('(pointer: fine)').matches;
function focusKbd() {
  if (!finePointer || !sessionActive() || document.activeElement === kbd) return;
  kbd.value = SENT; kbdOld = SENT;
  kbd.focus({ preventScroll: true });
}

// ------------------------------------------------------------------ toolbar
const KEYS = [
  ['▲ Kaydır', () => mouse('wheel', S.lastPt, 0, 240)], ['▼ Kaydır', () => mouse('wheel', S.lastPt, 0, -240)],
  ['Esc', () => tap(27)], ['Tab', () => tap(9)], ['Win', () => tap(91)], ['Enter', () => tap(13)], ['Sil', () => tap(46)],
  ['←', () => tap(37)], ['↑', () => tap(38)], ['→', () => tap(39)], ['↓', () => tap(40)],
  ['Ctrl+C', () => combo([17, 67])], ['Ctrl+V', () => combo([17, 86])], ['Ctrl+Z', () => combo([17, 90])],
  ['Ctrl+A', () => combo([17, 65])], ['Alt+Tab', () => combo([18, 9])], ['F5', () => tap(116)],
  ['Ctrl+Alt+Del', () => combo([17, 18, 46])],
];
function buildKeybar() {
  const kb = $('#keybar');
  for (const [label, fn] of KEYS) {
    const b = document.createElement('button');
    b.textContent = label; b.onclick = fn;
    kb.appendChild(b);
  }
}

let chatOpen = false;
function renderChat() {
  const log = $('#chatLog');
  if (!log) return;
  log.innerHTML = S.chat.map((m) => `<div>${esc(m)}</div>`).join('') || '<span class="muted small">Henüz mesaj yok.</span>';
  log.scrollTop = log.scrollHeight;
}
async function openChat() {
  chatOpen = true; S.unread = false; $('#tChat').textContent = '💬';
  const p = dialog('Sohbet', '<div id="chatLog" class="chatlog"></div><input id="chatIn" placeholder="Mesaj" autocomplete="off">',
    [['Kapat', null], ['Gönder', 'send', 'primary']]);
  renderChat();
  $('#chatIn').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); sendChat(); } });
  const v = await p;
  chatOpen = false;
  if (v === 'send') { sendChat(); openChat(); }
}
function sendChat() {
  const inp = $('#chatIn');
  const t = (inp?.value || '').trim().slice(0, 2000);
  if (!t) return;
  if (sendCtl('CHAT:' + t)) S.chat.push('Siz: ' + t);
  inp.value = ''; renderChat();
}

async function openMonitors() {
  const btns = [];
  for (let i = 0; i < S.monitors; i++) btns.push([`Monitör ${i + 1}${i === S.curMon ? ' ✓' : ''}`, i]);
  btns.push(['Kapat', null]);
  const v = await dialog('Monitör seç', '<p class="muted small">Seçim oturumdaki herkesin görüntüsünü değiştirir.</p>', btns);
  if (typeof v === 'number') sendCtl('MONITOR:' + v);
}

async function sasCode() {
  const pc = S.pc;
  if (!pc || !pc.localDescription || !pc.remoteDescription) return '';
  const fp = (sdp) => ((sdp.split('\n').map((l) => l.trim()).find((l) => l.startsWith('a=fingerprint:')) || '').slice(14)).toLowerCase();
  const a = [fp(pc.localDescription.sdp), fp(pc.remoteDescription.sdp)];
  if (!a[0] || !a[1]) return '';
  a.sort();
  const h = new Uint8Array(await crypto.subtle.digest('SHA-256', enc.encode(`remotesupport-sas/v1|${S.code.trim()}|${a[0]}|${a[1]}`)));
  const n = ((h[0] * 16777216) + (h[1] << 16) + (h[2] << 8) + h[3]) % 1000000;
  const s = String(n).padStart(6, '0');
  return s.slice(0, 3) + ' ' + s.slice(3);
}

async function statsLine() {
  const pc = S.pc;
  if (!pc) return ['Bağlı değil'];
  const out = [];
  try {
    const st = await pc.getStats();
    let pair = null; const cands = {};
    st.forEach((r) => {
      if (r.type === 'transport' && r.selectedCandidatePairId) pair = st.get(r.selectedCandidatePairId);
      if (r.type === 'local-candidate' || r.type === 'remote-candidate') cands[r.id] = r;
    });
    if (!pair) st.forEach((r) => { if (r.type === 'candidate-pair' && r.nominated && r.state === 'succeeded') pair = r; });
    if (pair) {
      const relay = [cands[pair.localCandidateId], cands[pair.remoteCandidateId]].some((c) => c && c.candidateType === 'relay');
      out.push('Yol: ' + (relay ? 'sunucu aktarmalı (TURN)' : 'doğrudan'));
      if (pair.currentRoundTripTime) out.push(`Gecikme: ${Math.round(pair.currentRoundTripTime * 1000)} ms`);
    } else out.push('Yol: belirleniyor');
  } catch { out.push('Yol: bilinmiyor'); }
  const now = performance.now();
  if (S.statAt) {
    const dt = (now - S.statAt) / 1000;
    if (dt > 0.2) {
      out.push(`Kare: ${Math.round((S.frames - S.statF) / dt)}/sn`);
      out.push(`Veri: ${Math.round((S.bytes - S.statB) / dt / 1024)} KB/sn`);
    }
  }
  S.statAt = now; S.statF = S.frames; S.statB = S.bytes;
  return out;
}

async function openInfo() {
  let open = true;
  const sas = await sasCode();
  const body = () => `<div id="infoLines"></div>${sas ? `<div>Güvenlik kodu</div><div class="sas">${sas}</div><p class="muted small">Karşı taraftaki NexDesk ekranında da aynı kod görünmeli. Farklıysa bağlantıyı kesin.</p>` : ''}`;
  const p = dialog('Bağlantı bilgisi', body(), [['Kapat', null]]);
  const tick = async () => {
    if (!open) return;
    const el = $('#infoLines');
    if (el) el.innerHTML = (await statsLine()).map((l) => `<div>${esc(l)}</div>`).join('');
    setTimeout(tick, 1000);
  };
  tick();
  await p; open = false;
}

// ------------------------------------------------------------------ wiring
function init() {
  $('#name').value = store.get('name', '');
  $('#name').addEventListener('change', () => store.set('name', $('#name').value.trim()));
  $('#btnConnect').onclick = () => {
    const c = normCode($('#code').value);
    if (!c) { notice('Kod 6–14 haneli olmalı.'); return; }
    if (needName()) connect({ code: c, title: c });
  };
  $('#code').addEventListener('keydown', (e) => { if (e.key === 'Enter') $('#btnConnect').click(); });
  $('#btnAddDev').onclick = addDevice;
  $('#btnSettings').onclick = settings;
  $('#ovCancel').onclick = () => end('');
  $('#tClose').onclick = () => end('');
  $('#tKeys').onclick = () => { show($('#keybar'), $('#keybar').classList.contains('hidden')); setTimeout(resizeView, 0); };
  $('#tKbd').onclick = () => {
    if (document.activeElement === kbd) { kbd.blur(); return; }
    kbd.value = SENT; kbdOld = SENT; kbd.focus(); kbd.setSelectionRange(SENT.length, SENT.length);
  };
  $('#tChat').onclick = openChat;
  $('#tMon').onclick = openMonitors;
  $('#tInfo').onclick = openInfo;
  buildKeybar();
  renderDevices();
  window.addEventListener('resize', resizeView);
  window.visualViewport?.addEventListener('resize', resizeView);
  document.addEventListener('visibilitychange', () => { if (!document.hidden && S.announced && !S.stopped) sendCtl('KEYFRAME'); });
  // Links like https://server/#code=1234567890 open straight into a session.
  const params = new URLSearchParams(location.hash.slice(1) || location.search.slice(1));
  const linkCode = normCode(params.get('code'));
  if (linkCode) {
    $('#code').value = linkCode;
    history.replaceState(null, '', location.pathname);
    if (store.get('name', '')) connect({ code: linkCode, title: linkCode });
    else notice('Adınızı girip Bağlan\'a basın.');
  }
  const standalone = window.matchMedia('(display-mode: standalone)').matches || navigator.standalone;
  show($('#installHint'), !standalone && /iphone|ipad/i.test(navigator.userAgent));
  if ('serviceWorker' in navigator && (location.protocol === 'https:' || location.hostname === 'localhost')) {
    navigator.serviceWorker.register('sw.js').catch(() => {});
  }
}
init();

// Exposed for the end-to-end test harness only.
window.__nexdesk = { S, sasCode };
