import { useMemo, type ReactNode } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { json } from '@codemirror/lang-json'
import { xml } from '@codemirror/lang-xml'
import { EditorView } from '@codemirror/view'
import { InfoTip } from '@/components/ui/info-tip'

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
  help,
  ariaInvalid,
  ariaDescribedBy,
}: {
  value: string
  onChange?: (value: string) => void
  language?: 'json' | 'xml' | 'text'
  label: string
  height?: string
  readOnly?: boolean
  help?: ReactNode
  ariaInvalid?: boolean
  ariaDescribedBy?: string
}) {
  const extensions = useMemo(
    () => [
      editorTheme,
      EditorView.lineWrapping,
      EditorView.contentAttributes.of({
        'aria-label': label,
        ...(ariaInvalid ? { 'aria-invalid': 'true' } : {}),
        ...(ariaDescribedBy ? { 'aria-describedby': ariaDescribedBy } : {}),
      }),
      ...(language === 'json' ? [json()] : language === 'xml' ? [xml()] : []),
    ],
    [language, label, ariaInvalid, ariaDescribedBy],
  )
  return (
    <div className="min-w-0 space-y-2">
      {help && (
        <div className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
          <span>{label}</span>
          <InfoTip label={label}>{help}</InfoTip>
        </div>
      )}
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
    </div>
  )
}
