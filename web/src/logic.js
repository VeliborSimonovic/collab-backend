import {Annotation, EditorState} from "@codemirror/state"

export const remoteAnnotation = Annotation.define()

export function u16ToCp(text, off) {
  let cp = 0
  const end = Math.min(off, text.length)
  for (let i = 0; i < end; i++) {
    const c = text.charCodeAt(i)
    if (c >= 0xd800 && c <= 0xdbff && i + 1 < text.length) {
      const d = text.charCodeAt(i + 1)
      if (d >= 0xdc00 && d <= 0xdfff) i++
    }
    cp++
  }
  return cp
}

export function cpToU16(text, cp) {
  let i = 0
  while (cp > 0 && i < text.length) {
    const c = text.charCodeAt(i)
    if (c >= 0xd800 && c <= 0xdbff && i + 1 < text.length) {
      const d = text.charCodeAt(i + 1)
      if (d >= 0xdc00 && d <= 0xdfff) i++
    }
    i++
    cp--
  }
  return i
}

export function cpLength(text) {
  return u16ToCp(text, text.length)
}

const hasSurrogate = (s) => /[\ud800-\udfff]/.test(s)
export {hasSurrogate}

export function extractChanges(oldText, changes) {
  const list = []
  changes.iterChanges((fromA, toA, fromB, toB, inserted) => {
    list.push({
      from: u16ToCp(oldText, fromA),
      to: u16ToCp(oldText, toA),
      insert: inserted.toString(),
    })
  })
  return list.reverse()
}

export function diffRemote(before, after) {
  if (before === after) return null
  const a = Array.from(before)
  const b = Array.from(after)
  const min = Math.min(a.length, b.length)
  let p = 0
  while (p < min && a[p] === b[p]) p++
  let s = 0
  while (s < min - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++
  const toCp = a.length - s
  return {
    fromCp: p,
    toCp,
    insert: b.slice(p, b.length - s).join(""),
    from: cpToU16(before, p),
    to: cpToU16(before, toCp),
  }
}

export function limitFilter(maxText, onLimitHit) {
  return EditorState.changeFilter.of((tr) => {
    if (!(maxText > 0) || !tr.docChanged) return true
    if (tr.annotation(remoteAnnotation)) return true
    const n = tr.newDoc.length
    if (n <= tr.startState.doc.length) return true
    if (n <= maxText) return true 
    if (cpLength(tr.newDoc.toString()) <= maxText) return true
    if (onLimitHit) onLimitHit()
    return false
  })
}
