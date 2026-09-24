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

const statusEl = document.getElementById("status");
const editor = document.getElementById("editor");
const usersEl = document.getElementById("users");

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
  if (removed > 0) send(2, doc.del(prefix, removed));
  if (inserted !== "") send(2, doc.insert(prefix, inserted));
  oldText = newArr;
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

function renderUsers() {
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
      send(2, doc.diff(payload));
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
  ws = new WebSocket(`${scheme}//${location.host}/ws?doc=demo&name=${encodeURIComponent(name)}`);
  ws.binaryType = "arraybuffer";
  ws.onopen = () => {
    statusEl.textContent = `connected as ${name}`;
    retryMs = 1000;
    send(1, doc.stateVector());
    sendPresence();
  };
  ws.onmessage = onMessage;
  ws.onclose = () => {
    statusEl.textContent = `offline, reconnecting in ${retryMs / 1000} s (you can keep typing)`;
    remoteCursors.clear();
    renderUsers();
    setTimeout(connect, retryMs);
    retryMs = Math.min(retryMs * 2, 10000);
  };
}

async function start() {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(fetch("collab.wasm"), go.importObject);
  go.run(result.instance);

  myId = Math.floor(Math.random() * 2 ** 48) + 2 ** 20;
  name = "user-" + Math.floor(Math.random() * 1000);
  doc = yata.newDoc(myId);
  oldText = [];

  editor.addEventListener("input", onInput);
  editor.addEventListener("click", sendPresence);
  editor.addEventListener("keyup", sendPresence);
  document.addEventListener("selectionchange", sendPresence);

  editor.disabled = false;
  connect();
}

start();
