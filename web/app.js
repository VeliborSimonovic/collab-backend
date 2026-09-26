"use strict";

let doc = null;
let ws = null;
let myId = 0;
let name = "";
let oldText = [];
const remoteCursors = new Map();
let retryMs = 1000;
let lastPresence = 0;
let presenceTimer = null;
let docId = "demo";   
let token = null;     
let role = "editor";
let maxText = 0;
let maxPeople = 0;
let demoOn = false;
let roomCode = "";
let expMs = 0;
let countdownTimer = null;
let noticeTimer = null;

const statusEl = document.getElementById("status");
const editor = document.getElementById("editor");
const usersEl = document.getElementById("users");
const landingEl = document.getElementById("landing");
const editorViewEl = document.getElementById("editorView");
const landingErrorEl = document.getElementById("landingError");
const roomInfoEl = document.getElementById("roomInfo");
const countdownEl = document.getElementById("countdown");
const peopleEl = document.getElementById("people");
const charCountEl = document.getElementById("charCount");
const noticeEl = document.getElementById("notice");

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function frame(type, bytes) {
  const out = new Uint8Array(bytes.length + 1);
  out[0] = type;
  out.set(bytes, 1);
  return out;
}

function send(type, bytes) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(frame(type, bytes));
  }
}

function toCodepointIndex(text, utf16Offset) {
  return Array.from(text.slice(0, utf16Offset)).length;
}

function toUtf16Offset(text, cpIndex) {
  return Array.from(text).slice(0, cpIndex).join("").length;
}

function diffText(oldArr, newArr) {
  const minLen = Math.min(oldArr.length, newArr.length);
  let prefix = 0;
  while (prefix < minLen && oldArr[prefix] === newArr[prefix]) prefix++;
  let suffix = 0;
  while (
    suffix < minLen - prefix &&
    oldArr[oldArr.length - 1 - suffix] === newArr[newArr.length - 1 - suffix]
  ) suffix++;
  return {
    prefix,
    removed: oldArr.length - prefix - suffix,
    inserted: newArr.slice(prefix, newArr.length - suffix).join(""),
  };
}

function onInput() {
  const newArr = Array.from(editor.value);
  const { prefix, removed, inserted } = diffText(oldText, newArr);
  if (maxText > 0 && newArr.length > maxText && newArr.length > oldText.length) {
    editor.value = oldText.join("");
    showNotice(`Document is full (${maxText} characters)`);
    return;
  }
  if (removed > 0) send(2, doc.del(prefix, removed));
  if (inserted !== "") send(2, doc.insert(prefix, inserted));
  oldText = newArr;
  updateCharCount();
  sendPresence();
}

function applyRemote(payload) {
  const text = editor.value;
  const startAnchor = doc.cursorId(toCodepointIndex(text, editor.selectionStart));
  const endAnchor = doc.cursorId(toCodepointIndex(text, editor.selectionEnd));

  const err = doc.apply(payload);
  if (typeof err === "string") console.error(err);

  const newText = doc.text();
  editor.value = newText;
  oldText = Array.from(newText);

  if (startAnchor !== null && endAnchor !== null) {
    const s = doc.cursorPos(startAnchor[0], startAnchor[1]);
    const e = doc.cursorPos(endAnchor[0], endAnchor[1]);
    if (s >= 0 && e >= 0) {
      editor.setSelectionRange(toUtf16Offset(newText, s), toUtf16Offset(newText, e));
    }
  }
  renderUsers();
  updateCharCount();
}

function sendPresence() {
  const wait = 100 - (Date.now() - lastPresence);
  if (wait > 0) {
    if (presenceTimer === null) {
      presenceTimer = setTimeout(() => {
        presenceTimer = null;
        sendPresence();
      }, wait);
    }
    return;
  }
  if (!doc) return;
  const text = editor.value;
  const anchor = doc.cursorId(toCodepointIndex(text, editor.selectionStart));
  const head = doc.cursorId(toCodepointIndex(text, editor.selectionEnd));
  if (anchor === null || head === null) return;
  lastPresence = Date.now();
  send(3, encoder.encode(JSON.stringify({ client: myId, anchor, head })));
}

function updateCharCount() {
  if (maxText > 0) charCountEl.textContent = `${oldText.length} / ${maxText}`;
}

function showNotice(msg) {
  noticeEl.textContent = msg;
  clearTimeout(noticeTimer);
  noticeTimer = setTimeout(() => { noticeEl.textContent = ""; }, 3000);
}

function formatCode(code) {
  return code.length === 6 ? code.slice(0, 3) + " " + code.slice(3) : code;
}

function updateCountdown() {
  const left = Math.max(0, Math.ceil((expMs - Date.now()) / 1000));
  const m = Math.floor(left / 60);
  const s = String(left % 60).padStart(2, "0");
  countdownEl.textContent = left > 0 ? `Demo ends in ${m}:${s}` : "Demo ended";
  if (left === 0) clearInterval(countdownTimer);
}

function showEnded(msg) {
  statusEl.textContent = msg + " ";
  const a = document.createElement("a");
  a.href = "/";
  a.textContent = "Back to the start";
  statusEl.append(a);
}

function renderUsers() {
  if (maxPeople > 0) peopleEl.textContent = `${1 + remoteCursors.size} / ${maxPeople} people`;
  usersEl.replaceChildren();
  for (const [, u] of remoteCursors) {
    const li = document.createElement("li");
    const dot = document.createElement("span");
    dot.style.cssText =
      "display:inline-block;width:10px;height:10px;border-radius:50%;margin-right:6px;";
    dot.style.background = u.color;
    const pos = u.head ? doc.cursorPos(u.head[0], u.head[1]) : -1;
    li.append(dot, `${u.name}: ${pos < 0 ? "position unknown" : "at position " + pos}`);
    usersEl.append(li);
  }
}

function onMessage(ev) {
  const data = new Uint8Array(ev.data);
  if (data.length === 0) return;
  const type = data[0];
  const payload = data.subarray(1);
  switch (type) {
    case 1:
      if (role !== "viewer") send(2, doc.diff(payload));
      break;
    case 2:
      applyRemote(payload);
      break;
    case 3: {
      const p = JSON.parse(decoder.decode(payload));
      remoteCursors.set(p.client, { name: p.name, color: p.color, head: p.head });
      renderUsers();
      break;
    }
    case 4: {
      const p = JSON.parse(decoder.decode(payload));
      remoteCursors.delete(p.client);
      renderUsers();
      break;
    }
  }
}

function connect() {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const q = token
    ? `doc=${encodeURIComponent(docId)}&token=${encodeURIComponent(token)}`
    : `doc=${encodeURIComponent(docId)}&name=${encodeURIComponent(name)}`;
  ws = new WebSocket(`${scheme}//${location.host}/ws?${q}`);
  ws.binaryType = "arraybuffer";
  ws.onopen = () => {
    statusEl.textContent = `connected to ${docId} as ${name} (${role})`;
    retryMs = 1000;
    send(1, doc.stateVector());
    sendPresence();
  };
  ws.onmessage = onMessage;
  ws.onclose = (ev) => {
    if (ev.code === 4001 || ev.code === 4002 || ev.code === 4003) {
      remoteCursors.clear();
      renderUsers();
      if (ev.code === 4001) {
        editor.readOnly = true;
        clearInterval(countdownTimer);
        showEnded("Demo ended — thanks for trying it!");
      } else if (ev.code === 4002) {
        statusEl.textContent = `This document is full (${maxText} characters). Reload the page to rejoin.`;
      } else {
        statusEl.textContent = "Too many edits at once. Reload the page to rejoin.";
      }
      return;
    }
    statusEl.textContent = `offline, reconnecting in ${retryMs / 1000} s (you can keep typing)`;
    remoteCursors.clear();
    renderUsers();
    setTimeout(connect, retryMs);
    retryMs = Math.min(retryMs * 2, 10000);
  };
}

async function loadConfig() {
  try {
    const res = await fetch("/demo/config");
    if (!res.ok) return;
    const cfg = await res.json();
    demoOn = cfg.enabled === true;
    if (demoOn) {
      maxText = cfg.maxText || 0;
      maxPeople = cfg.maxPeople || 0;
      if (cfg.maxPeople) document.getElementById("landingPeople").textContent = cfg.maxPeople;
      if (cfg.ttlSeconds) document.getElementById("landingMinutes").textContent = Math.round(cfg.ttlSeconds / 60);
    }
  } catch (e) {
    console.error("could not load /demo/config", e);
  }
}

function retryMinutes(res) {
  const secs = parseInt(res.headers.get("Retry-After"), 10);
  return Math.max(1, Math.ceil((isNaN(secs) ? 60 : secs) / 60));
}

async function landingPost(btn, path, body, limitMsg) {
  landingErrorEl.textContent = "";
  btn.disabled = true;
  try {
    const res = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (res.ok) {
      const data = await res.json();
      location.href = data.url;
      return;
    }
    if (res.status === 429) {
      const min = retryMinutes(res);
      landingErrorEl.textContent = limitMsg(min);
    } else {
      landingErrorEl.textContent = (await res.text()).trim() || "Something went wrong.";
    }
  } catch (e) {
    landingErrorEl.textContent = "Could not reach the server.";
  }
  btn.disabled = false;
}

function showLanding(joinCode) {
  landingEl.hidden = false;
  const startName = document.getElementById("startName");
  const joinCodeEl = document.getElementById("joinCode");
  const joinName = document.getElementById("joinName");
  const plural = (n) => `${n} minute${n === 1 ? "" : "s"}`;

  document.getElementById("startBtn").addEventListener("click", (ev) => {
    landingPost(ev.target, "/demo/rooms", { name: startName.value }, (min) =>
      `You can start one room per hour. Try again in ${plural(min)}, or join someone's room with a code.`);
  });
  document.getElementById("joinBtn").addEventListener("click", (ev) => {
    landingPost(ev.target, "/demo/rooms/join", { code: joinCodeEl.value, name: joinName.value }, (min) =>
      `Too many wrong codes. Try again in ${plural(min)}.`);
  });

  if (joinCode) {
    joinCodeEl.value = formatCode(joinCode.replace(/\D/g, ""));
    joinName.focus();
  }
}

async function start() {
  const params = new URLSearchParams(location.search);
  token = params.get("token");

  await loadConfig();
  if (!token && demoOn) {
    showLanding(params.get("join"));
    return;
  }

  editorViewEl.hidden = false;

  if (token) {
    try {
      const payload = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
      const claims = JSON.parse(atob(payload));
      name = claims.name || claims.sub || name;
      role = claims.role || role;
      expMs = (claims.exp || 0) * 1000;
    } catch (e) {
      console.error("could not read token payload", e);
    }
  }
  if (expMs > 0 && expMs <= Date.now()) {
    editor.hidden = true;
    showEnded("This demo has ended.");
    return;
  }

  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(fetch("collab.wasm"), go.importObject);
  go.run(result.instance);

  myId = Math.floor(Math.random() * 2 ** 48) + 2 ** 20;
  if (name === "") name = "user-" + Math.floor(Math.random() * 1000);
  docId = params.get("doc") || "demo";
  roomCode = params.get("code") || "";

  if (roomCode) {
    document.getElementById("roomCode").textContent = formatCode(roomCode);
    roomInfoEl.hidden = false;
    document.getElementById("copyInvite").addEventListener("click", async (ev) => {
      try {
        await navigator.clipboard.writeText(location.origin + "/?join=" + roomCode);
        ev.target.textContent = "Copied!";
      } catch (e) {
        ev.target.textContent = "Copy failed";
      }
      setTimeout(() => { ev.target.textContent = "Copy invite link"; }, 2000);
    });
  }
  if (expMs > 0) {
    countdownEl.hidden = false;
    updateCountdown();
    countdownTimer = setInterval(updateCountdown, 1000);
  }
  if (maxPeople > 0) peopleEl.hidden = false;
  if (maxText > 0) {
    charCountEl.hidden = false;
    editor.maxLength = maxText;
  }

  if (role === "viewer") editor.readOnly = true;
  document.title = `${docId} · ${name} (${role})`;
  doc = yata.newDoc(myId);
  oldText = [];
  renderUsers();
  updateCharCount();

  editor.addEventListener("input", onInput);
  editor.addEventListener("click", sendPresence);
  editor.addEventListener("keyup", sendPresence);
  document.addEventListener("selectionchange", sendPresence);

  editor.disabled = false;
  connect();
}

start();
