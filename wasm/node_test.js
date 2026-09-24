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
  process.exit(0);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
