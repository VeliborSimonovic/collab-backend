import {test} from "node:test"
import assert from "node:assert/strict"
import {EditorState} from "@codemirror/state"
import {
  remoteAnnotation, u16ToCp, cpToU16, extractChanges, diffRemote, limitFilter,
} from "./logic.js"

const mixed = "ač😀b😀"


test("u16ToCp / cpToU16 table", () => {
  assert.equal(u16ToCp(mixed, 0), 0)
  assert.equal(u16ToCp(mixed, 2), 2)
  assert.equal(u16ToCp(mixed, 4), 3)
  assert.equal(u16ToCp(mixed, 5), 4)
  assert.equal(u16ToCp(mixed, 7), 5)
  assert.equal(cpToU16(mixed, 3), 4)
  assert.equal(cpToU16(mixed, 5), 7)
  assert.equal(cpToU16(mixed, 99), 7)
})

test("round trip over every codepoint boundary", () => {
  const n = Array.from(mixed).length
  for (let cp = 0; cp <= n; cp++) assert.equal(u16ToCp(mixed, cpToU16(mixed, cp)), cp)
})

test("change extraction", () => {
  const s = EditorState.create({doc: "hello"})
  const tr = s.update({changes: {from: 1, to: 3, insert: "XY"}})
  assert.deepEqual(extractChanges("hello", tr.changes), [{from: 1, to: 3, insert: "XY"}])
})

test("changes come out in descending order", () => {
  const s = EditorState.create({doc: "hello"})
  const tr = s.update({changes: [{from: 0, to: 1, insert: ""}, {from: 4, to: 5, insert: "Z"}]})
  const out = extractChanges("hello", tr.changes)
  assert.equal(out.length, 2)
  assert.equal(out[0].from, 4)
  assert.equal(out[1].from, 0)
})

test("remote diff", () => {
  const d = diffRemote("hello", "heLLo")
  assert.deepEqual([d.fromCp, d.toCp, d.insert], [2, 4, "LL"])
  assert.equal(diffRemote("hello", "hello"), null)
})

test("astral remote diff uses UTF-16 offsets", () => {
  const d = diffRemote("a😀b", "a😀Xb")
  assert.equal(d.from, 3)
  assert.equal(d.fromCp, 2)
  assert.equal(d.insert, "X")
})

test("limit filter", () => {
  let hits = 0
  const s = EditorState.create({
    doc: "abcde",
    extensions: [limitFilter(5, () => hits++)],
  })
  assert.equal(s.update({changes: {from: 5, insert: "f"}}).newDoc.toString(), "abcde")
  assert.equal(hits, 1)
  assert.equal(s.update({changes: {from: 0, to: 2}}).newDoc.toString(), "cde")
  const remote = s.update({changes: {from: 5, insert: "f"}, annotations: remoteAnnotation.of(true)})
  assert.equal(remote.newDoc.toString(), "abcdef")
  assert.equal(hits, 1)
})

test("limit counts codepoints, not UTF-16 units", () => {
  const s = EditorState.create({doc: "abc", extensions: [limitFilter(5, null)]})
  // 2 emoji = 4 units but 2 codepoints -> 5 codepoints total, allowed
  assert.equal(s.update({changes: {from: 3, insert: "😀😀"}}).newDoc.toString(), "abc😀😀")
})
