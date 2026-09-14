// CodeMirror wiring, kept out of the component so the component stays about
// layout and the editor setup stays testable by inspection.
import { EditorView, basicSetup } from 'codemirror'
import { EditorState, type Extension } from '@codemirror/state'
import { yaml } from '@codemirror/lang-yaml'
import { json } from '@codemirror/lang-json'
import { markdown } from '@codemirror/lang-markdown'
import { html } from '@codemirror/lang-html'
import { css } from '@codemirror/lang-css'
import { javascript } from '@codemirror/lang-javascript'

/** Language extension for a mode name from the server's textfile.Language. */
function languageOf(mode: string): Extension[] {
  switch (mode) {
    case 'yaml':
      return [yaml()]
    case 'json':
      return [json()]
    case 'markdown':
      return [markdown()]
    case 'html':
      return [html()]
    case 'css':
      return [css()]
    case 'javascript':
      return [javascript()]
    default:
      return []
  }
}

export interface EditorHandle {
  view: EditorView
  getValue(): string
  destroy(): void
  /** Put the cursor on a 1-based line, for jumping to a reported syntax error. */
  goToLine(line: number): void
}

export function createEditor(
  parent: HTMLElement,
  doc: string,
  mode: string,
  onChange: () => void,
): EditorHandle {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        basicSetup,
        ...languageOf(mode),
        EditorView.lineWrapping,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) onChange()
        }),
        EditorView.theme({
          '&': { height: '100%', fontSize: '13px' },
          '.cm-scroller': { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' },
        }),
      ],
    }),
  })

  return {
    view,
    getValue: () => view.state.doc.toString(),
    destroy: () => view.destroy(),
    goToLine(line: number) {
      // Clamp: the server's reported line can exceed the document if the user
      // deleted lines between the failed save and the jump.
      const n = Math.min(Math.max(line, 1), view.state.doc.lines)
      const pos = view.state.doc.line(n).from
      view.dispatch({ selection: { anchor: pos }, scrollIntoView: true })
      view.focus()
    },
  }
}
