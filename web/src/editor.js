import {Compartment, EditorState, StateEffect, StateField, Transaction} from "@codemirror/state"
import {Decoration, EditorView, WidgetType} from "@codemirror/view"
import {markdown} from "@codemirror/lang-markdown"
import {basicSetup} from "codemirror"
import {
  remoteAnnotation, u16ToCp, cpToU16, cpLength, hasSurrogate,
  extractChanges, diffRemote, limitFilter,
} from "./logic.js"

class CaretWidget extends WidgetType {
  constructor(name, color) {
    super()
    this.name = name
    this.color = color
  }
  toDOM() {
    const caret = document.createElement("span")
    caret.className = "cm-remote-caret"
    caret.style.borderLeftColor = this.color
    caret.style.background = this.color
    const label = document.createElement("span")
    label.className = "cm-remote-name"
    label.textContent = this.name
    caret.append(label)
    return caret
  }
  eq(other) {
    return other.name === this.name && other.color === this.color
  }
  ignoreEvent() {
    return false
  }
}

const setCursors = StateEffect.define()

const remoteCursorField = StateField.define({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes)
    for (const e of tr.effects) {
      if (e.is(setCursors)) {
        deco = Decoration.set(
          e.value.map((c) =>
            Decoration.widget({widget: new CaretWidget(c.name, c.color), side: -1}).range(c.pos)),
          true)
      }
    }
    return deco
  },
  provide: (f) => EditorView.decorations.from(f),
})

const editableCompartment = new Compartment()

function mount(options) {
  const {parent, doc, maxText = 0, onLocalOps, onCursor, onLimitHit} = options
  let hasAstral = hasSurrogate(doc.text())

  const listener = EditorView.updateListener.of((update) => {
    if (update.docChanged) {
      if (!hasAstral) {
        update.changes.iterChanges((fA, tA, fB, tB, inserted) => {
          if (!hasAstral && hasSurrogate(inserted.toString())) hasAstral = true
        })
      }
      if (update.transactions.some((tr) => tr.annotation(remoteAnnotation))) return
      const oldText = update.startState.doc.toString()
      for (const c of extractChanges(oldText, update.changes)) {
        if (c.to > c.from) onLocalOps(doc.del(c.from, c.to - c.from))
        if (c.insert !== "") onLocalOps(doc.insert(c.from, c.insert))
      }
    }
    if ((update.selectionSet || update.docChanged) && onCursor) onCursor()
  })

  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: doc.text(),
      extensions: [
        basicSetup,
        markdown(),
        EditorView.lineWrapping,
        listener,
        limitFilter(maxText, onLimitHit),
        remoteCursorField,
        editableCompartment.of(EditorView.editable.of(true)),
        EditorView.theme({
          "&": {height: "min(60vh, 520px)", fontSize: "15px"},
          "&.cm-focused": {outline: "none"},
          ".cm-scroller": {overflow: "auto", fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", lineHeight: "1.6"},
          ".cm-content": {padding: "12px 0"},
          ".cm-gutters": {background: "#fafbfe", borderRight: "1px solid #e3e6ee", color: "#9aa1b5"},
        }),
      ],
    }),
  })

  return {
    view,
    applyRemote(bytes) {
      const before = doc.text()
      const err = doc.apply(bytes)
      if (typeof err === "string") console.error(err)
      const d = diffRemote(before, doc.text())
      if (!d) return
      view.dispatch({
        changes: {from: d.from, to: d.to, insert: d.insert},
        annotations: [remoteAnnotation.of(true), Transaction.addToHistory.of(false)],
      })
    },
    localCursor() {
      const text = view.state.doc.toString()
      const sel = view.state.selection.main
      const anchor = doc.cursorId(u16ToCp(text, sel.anchor))
      const head = doc.cursorId(u16ToCp(text, sel.head))
      if (anchor === null || head === null) return null
      return {anchor, head}
    },
    setRemoteCursors(list) {
      const text = view.state.doc.toString()
      const out = []
      for (const u of list) {
        if (!u.head) continue
        const cp = doc.cursorPos(u.head[0], u.head[1])
        if (cp < 0) continue
        out.push({name: u.name, color: u.color, pos: Math.min(cpToU16(text, cp), text.length)})
      }
      view.dispatch({effects: setCursors.of(out)})
    },
    setEditable(flag) {
      view.dispatch({effects: editableCompartment.reconfigure(EditorView.editable.of(flag))})
    },
    charCount() {
      const d = view.state.doc
      return hasAstral ? cpLength(d.toString()) : d.length
    },
    destroy() {
      view.destroy()
    },
  }
}

export {mount}
