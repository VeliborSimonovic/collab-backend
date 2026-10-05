package web

import "embed"

//go:embed index.html app.js wasm_exec.js collab.wasm editor.js
var FS embed.FS
