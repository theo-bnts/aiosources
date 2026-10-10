import { useMemo } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { json } from '@codemirror/lang-json'
import { xml } from '@codemirror/lang-xml'
import { EditorView } from '@codemirror/view'

const editorTheme = EditorView.theme(
  {
    '&': { backgroundColor: '#161a1d', color: '#d6dddf' },
    '.cm-gutters': { backgroundColor: '#1b2023', color: '#869496' },
    '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: '#27312f' },
    '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: '#35554e' },
    '.cm-cursor': { borderLeftColor: '#87cabc' },
  },
  { dark: true },
)

export function CodeEditor({
  value,
  onChange,
  language = 'json',
  label,
  height = '460px',
  readOnly = false,
}: {
  value: string
  onChange?: (value: string) => void
  language?: 'json' | 'xml' | 'text'
  label: string
  height?: string
  readOnly?: boolean
}) {
  const extensions = useMemo(
    () => [
      editorTheme,
      EditorView.lineWrapping,
      EditorView.contentAttributes.of({ 'aria-label': label }),
      ...(language === 'json' ? [json()] : language === 'xml' ? [xml()] : []),
    ],
    [language, label],
  )
  return (
    <div className="editor-frame">
      <CodeMirror
        value={value}
        onChange={onChange}
        height={height}
        theme="dark"
        extensions={extensions}
        readOnly={readOnly}
        editable={!readOnly}
        basicSetup={{
          foldGutter: true,
          highlightActiveLine: !readOnly,
          highlightActiveLineGutter: !readOnly,
        }}
        indentWithTab={false}
      />
    </div>
  )
}
