const fs = require("fs");
const path = require("path");
require(path.join(__dirname, "..", "web", "wasm_exec.js"));

async function main() {
  const go = new Go();
  const wasm = fs.readFileSync(path.join(__dirname, "..", "web", "collab.wasm"));
  const { instance } = await WebAssembly.instantiate(wasm, go.importObject);
  go.run(instance);  

  const rid = () => Math.floor(Math.random() * 2 ** 48) + 2 ** 20;
  const A = yata.newDoc(rid());
  const B = yata.newDoc(rid());

  const a1 = A.insert(0, "hello");
  const b1 = B.insert(0, "world 🌍");
  const a2 = A.del(0, 1);

  for (const [doc, bytes] of [[B, a2], [B, a1], [A, b1]]) {
    const err = doc.apply(bytes);
    if (err !== null) {
      console.log("apply error:", err);
      process.exit(1);
    }
  }

  console.log("A:", A.text());
  console.log("B:", B.text());
  if (A.text() !== B.text()) {
    console.log("FAIL");
    process.exit(1);
  }
  console.log("OK: converged");

  const SA = yata.newDoc(rid());
  const SB = yata.newDoc(rid());
  const deck = "r:map:deck";

  const r1 = SA.mapSetType(deck, "s1", "map");
  const s = r1.ref;
  const r2 = SA.mapSetJSON(s, "title", '"Intro"');
  const r3 = SB.mapSetJSON(deck, "theme", '"dark"');

  for (const r of [r1, r2, r3]) {
    if (r.error !== null) {
      console.log("FAIL: local op error:", r.error);
      process.exit(1);
    }
  }

  // Reverse order: nested set arrives before its container exists.
  const t1 = JSON.parse(SB.applyTracked(r2.ops));
  const t2 = JSON.parse(SB.applyTracked(r1.ops));
  const t3 = JSON.parse(SA.applyTracked(r3.ops));
  for (const t of [t1, t2, t3]) {
    if (t.error !== null) {
      console.log("FAIL: applyTracked error:", t.error);
      process.exit(1);
    }
  }

  const ja = SA.toJSON(deck);
  const jb = SB.toJSON(deck);
  const want = { s1: { title: "Intro" }, theme: "dark" };
  console.log("SA:", ja);
  console.log("SB:", jb);
  if (
    ja !== jb ||
    JSON.stringify(JSON.parse(ja)) !== JSON.stringify(want) ||
    !t2.changed.includes(deck) ||
    !t2.changed.includes(s)
  ) {
    console.log("FAIL", t2);
    process.exit(1);
  }
  console.log("OK: shared types converged");
  process.exit(0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
