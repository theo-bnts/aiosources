import { createContext, useContext, useEffect, useId, useState, type ReactNode } from 'react'
import { PlusIcon, TrashIcon } from '@phosphor-icons/react'
import { validateJSONValue, type ProviderJSON, type ProviderPath } from '@/lib/provider-json'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { CodeEditor } from '@/components/code-editor'

export interface SourceEditorContextValue {
  document: ProviderJSON
  readOnly: boolean
  change: (edit: (document: ProviderJSON) => void) => void
  reportDraft: (id: string, invalid: boolean) => void
}
export const SourceEditorContext = createContext<SourceEditorContextValue | null>(null)
export function useSourceEditor() {
  const value = useContext(SourceEditorContext)
  if (!value) throw new Error('Source controls require an editor context.')
  return value
}

interface FieldProps {
  path: ProviderPath
  label: string
  help?: string
}

export function TextField({
  path,
  label,
  help,
  placeholder,
  readOnly = false,
}: FieldProps & { placeholder?: string; readOnly?: boolean }) {
  const { document, change } = useSourceEditor()
  const id = useId()
  const value = document.getIn(path)
  const mismatch = value !== undefined && typeof value !== 'string'
  return (
    <div className="field min-w-0">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={typeof value === 'string' ? value : ''}
        placeholder={placeholder}
        readOnly={readOnly}
        aria-describedby={help || mismatch ? `${id}-help` : undefined}
        onChange={(event) => change((next) => next.setIn(path, event.target.value))}
      />
      {(help || mismatch) && (
        <p id={`${id}-help`} className="help">
          {mismatch
            ? 'The current value is not text. Enter text to replace it, or repair it in JSON.'
            : help}
        </p>
      )}
    </div>
  )
}

export function SelectField({
  path,
  label,
  help,
  choices,
  fallback = 'Not set (use default)',
  labels = {},
}: FieldProps & {
  choices: readonly string[]
  fallback?: string
  labels?: Record<string, string>
}) {
  const { document, change } = useSourceEditor()
  const id = useId()
  const value = document.getIn(path)
  const current = typeof value === 'string' ? value : ''
  return (
    <div className="field min-w-0">
      <Label htmlFor={id}>{label}</Label>
      <select
        id={id}
        className="native-select w-full"
        value={current}
        aria-describedby={help ? `${id}-help` : undefined}
        onChange={(event) =>
          change((next) =>
            event.target.value ? next.setIn(path, event.target.value) : next.deleteIn(path),
          )
        }
      >
        <option value="">{fallback}</option>
        {current && !choices.includes(current) && (
          <option value={current}>{current} (current)</option>
        )}
        {choices.map((choice) => (
          <option key={choice} value={choice}>
            {labels[choice] ?? choice}
          </option>
        ))}
      </select>
      {help && (
        <p className="help" id={`${id}-help`}>
          {help}
        </p>
      )}
    </div>
  )
}

export function BooleanField({
  path,
  label,
  help,
  required = false,
  allowEnabled = true,
}: FieldProps & { required?: boolean; allowEnabled?: boolean }) {
  const { document, change } = useSourceEditor()
  const id = useId()
  const value = document.getIn(path)
  return (
    <div className="field">
      <div className="flex min-h-9 items-center justify-between gap-3">
        <Label htmlFor={id}>{label}</Label>
        <Switch
          id={id}
          checked={value === true}
          disabled={!allowEnabled && value !== true}
          aria-describedby={help ? `${id}-help` : undefined}
          onCheckedChange={(enabled) => change((next) => next.setIn(path, enabled))}
        />
      </div>
      {!required && typeof value === 'boolean' && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="self-start"
          onClick={() => change((next) => next.deleteIn(path))}
        >
          Use default
        </Button>
      )}
      {help && (
        <p className="help" id={`${id}-help`}>
          {help}
        </p>
      )}
    </div>
  )
}

// Invalid drafts are retained locally and block save/navigation until repaired or discarded.
// Valid fragments are spliced into the AST without converting numeric tokens to JS numbers.
export function JSONField({
  path,
  label,
  help,
  integer = false,
  multiline = false,
  required = typeof path[path.length - 1] === 'number',
  placeholder,
}: FieldProps & {
  integer?: boolean
  multiline?: boolean
  required?: boolean
  placeholder?: string
}) {
  const { document, change, reportDraft, readOnly } = useSourceEditor()
  const id = useId()
  const raw = document.rawIn(path)
  const [draft, setDraft] = useState(raw)
  const [error, setError] = useState('')
  useEffect(() => {
    setDraft(raw)
    setError('')
    reportDraft(id, false)
  }, [raw, id, reportDraft])
  useEffect(() => () => reportDraft(id, false), [id, reportDraft])
  function update(value: string) {
    setDraft(value)
    if (!value.trim() && !required) {
      change((next) => next.deleteIn(path))
      setError('')
      reportDraft(id, false)
      return
    }
    const invalid =
      validateJSONValue(value)[0] ||
      (integer && !/^-?(0|[1-9]\d*)$/.test(value.trim())
        ? 'Enter a whole number without quotes.'
        : '')
    setError(invalid)
    reportDraft(id, Boolean(invalid))
    if (!invalid) change((next) => next.setRawIn(path, value))
  }
  return (
    <div className="field min-w-0">
      <Label htmlFor={multiline ? undefined : id}>{label}</Label>
      {multiline ? (
        <CodeEditor
          value={draft}
          onChange={update}
          label={label}
          height="150px"
          readOnly={readOnly}
        />
      ) : (
        <Input
          id={id}
          className="font-mono text-xs"
          inputMode={integer ? 'numeric' : undefined}
          placeholder={placeholder}
          value={draft}
          aria-invalid={Boolean(error)}
          aria-describedby={error || help ? `${id}-help` : undefined}
          onChange={(event) => update(event.target.value)}
        />
      )}
      {error ? (
        <div id={`${id}-help`} className="space-y-1 text-xs text-destructive" role="alert">
          <p>{error} This draft has not been applied; save is blocked.</p>
          <Button
            variant="warning"
            size="sm"
            onClick={() => {
              setDraft(raw)
              setError('')
              reportDraft(id, false)
            }}
          >
            Discard field draft
          </Button>
        </div>
      ) : (
        help && (
          <p className="help" id={`${id}-help`}>
            {help}
          </p>
        )
      )}
    </div>
  )
}

export function FieldGrid({ children }: { children: ReactNode }) {
  return <div className="grid gap-5 sm:grid-cols-2">{children}</div>
}

export function FieldSection({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section
      data-settings-section={title}
      className="space-y-5 border-t border-border pt-6 first-of-type:border-0 first-of-type:pt-0"
    >
      <div>
        <h3 className="font-medium">{title}</h3>
        {description && <p className="help mt-1 max-w-2xl">{description}</p>}
      </div>
      {children}
    </section>
  )
}

export function AdvancedSection({
  title,
  description,
  children,
  configuredPaths = [],
}: {
  title: string
  description?: string
  children: ReactNode
  configuredPaths?: ProviderPath[]
}) {
  const { document } = useSourceEditor()
  const configured = configuredPaths.some((path) => {
    const raw = document.rawIn(path)
    return raw !== '' && !['null', '{}', '[]'].includes(raw)
  })
  return (
    <Disclosure
      data-settings-section={title}
      title={
        <span className="flex flex-wrap items-center justify-between gap-2">
          <span>{title}</span>
          {configured && (
            <span className="text-xs font-normal text-muted-foreground">Configured</span>
          )}
        </span>
      }
    >
      {description && <p className="help">{description}</p>}
      {children}
    </Disclosure>
  )
}

function MapRow({ path, name, strings }: { path: ProviderPath; name: string; strings: boolean }) {
  const { document, change, reportDraft } = useSourceEditor()
  const [key, setKey] = useState(name)
  const id = useId()
  const [error, setError] = useState('')
  useEffect(() => () => reportDraft(id, false), [id, reportDraft])
  return (
    <div className="grid min-w-0 items-start gap-3 rounded-md bg-background/50 p-3 sm:grid-cols-[minmax(0,0.8fr)_minmax(0,1fr)_auto]">
      <div className="field min-w-0">
        <Label htmlFor={id}>Key</Label>
        <Input
          id={id}
          className="font-mono text-xs"
          value={key}
          onChange={(event) => {
            setKey(event.target.value)
            reportDraft(id, event.target.value !== name)
          }}
        />
        {key !== name && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              if (!key || document.keysIn(path).includes(key)) {
                setError('Enter a nonempty, unique key.')
                return
              }
              change((next) => {
                next.setRawIn([...path, key], next.rawIn([...path, name]))
                next.deleteIn([...path, name])
              })
              reportDraft(id, false)
            }}
          >
            Rename key
          </Button>
        )}
        {key !== name && (
          <Button
            size="sm"
            variant="warning"
            onClick={() => {
              setKey(name)
              setError('')
              reportDraft(id, false)
            }}
          >
            Discard rename
          </Button>
        )}
        {error && (
          <p className="help text-destructive" role="alert">
            {error}
          </p>
        )}
      </div>
      {strings ? (
        <TextField path={[...path, name]} label={`Value for ${name}`} />
      ) : (
        <JSONField
          path={[...path, name]}
          label={`JSON value for ${name}`}
          help={'Use JSON: "text", 123, true, null, arrays or objects.'}
          required
        />
      )}
      <Button
        variant="warning"
        size="icon-sm"
        className="sm:mt-7"
        aria-label={`Remove ${name}`}
        onClick={() => change((next) => next.deleteIn([...path, name]))}
      >
        <TrashIcon />
      </Button>
    </div>
  )
}

export function MapField({
  path,
  label,
  help,
  strings = false,
  suggestions = [],
  excludeKeys = [],
}: FieldProps & {
  strings?: boolean
  suggestions?: readonly string[]
  excludeKeys?: readonly string[]
}) {
  const { document, change } = useSourceEditor()
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const id = useId()
  const allKeys = document.keysIn(path)
  const keys = allKeys.filter((key) => !excludeKeys.includes(key))
  const kind = document.kindIn(path)
  return (
    <div className="min-w-0 space-y-3">
      <div>
        <h4 className="text-sm font-medium">{label}</h4>
        {help && <p className="help mt-1">{help}</p>}
      </div>
      {kind && kind !== 'object' && kind !== 'null' ? (
        <JSONField
          path={path}
          label={`${label} JSON`}
          help="This value is not an object. Repair it here or in the full JSON definition."
          multiline
        />
      ) : (
        <>
          {keys.map((name) => (
            <MapRow key={name} path={path} name={name} strings={strings} />
          ))}
          {!keys.length && <p className="help">No entries configured.</p>}
          <div className="flex flex-wrap items-end gap-2">
            <div className="field min-w-0 flex-1">
              <Label htmlFor={id}>New key</Label>
              <Input
                id={id}
                value={key}
                onChange={(event) => {
                  setKey(event.target.value)
                  setError('')
                }}
                placeholder="Parameter or field name"
                list={suggestions.length ? `${id}-suggestions` : undefined}
              />
              {suggestions.length > 0 && (
                <datalist id={`${id}-suggestions`}>
                  {suggestions
                    .filter((value) => !keys.includes(value))
                    .map((value) => (
                      <option key={value} value={value} />
                    ))}
                </datalist>
              )}
            </div>
            <Button
              variant="outline"
              size="sm"
              disabled={!key}
              onClick={() => {
                if (allKeys.includes(key) || excludeKeys.includes(key)) {
                  setError('That key is already configured.')
                  return
                }
                change((next) => next.setIn([...path, key], ''))
                setKey('')
              }}
            >
              <PlusIcon />
              Add entry
            </Button>
          </div>
          {error && (
            <p className="help text-destructive" role="alert">
              {error}
            </p>
          )}
        </>
      )}
    </div>
  )
}

export function ArrayField({
  path,
  label,
  help,
  initial,
  children,
}: FieldProps & { initial: unknown; children: (path: ProviderPath, index: number) => ReactNode }) {
  const { document, change } = useSourceEditor()
  const count = document.lengthIn(path)
  const kind = document.kindIn(path)
  return (
    <div className="space-y-4">
      <div>
        <h4 className="text-sm font-medium">{label}</h4>
        {help && <p className="help mt-1">{help}</p>}
      </div>
      {kind && kind !== 'array' && kind !== 'null' ? (
        <JSONField path={path} label={`${label} JSON`} multiline />
      ) : (
        <>
          {Array.from({ length: count }, (_, index) => (
            <div className="space-y-4 border-l-2 border-border pl-4" key={index}>
              <div className="flex items-center justify-between gap-3">
                <span className="mono text-muted-foreground">
                  {label} · {index + 1}
                </span>
                <Button
                  variant="warning"
                  size="icon-sm"
                  aria-label={`Remove ${label} ${index + 1}`}
                  onClick={() => change((next) => next.deleteIn([...path, index]))}
                >
                  <TrashIcon />
                </Button>
              </div>
              {children([...path, index], index)}
            </div>
          ))}
          {!count && <p className="help">No entries configured.</p>}
          <Button
            variant="outline"
            size="sm"
            onClick={() => change((next) => next.setIn([...path, count], initial))}
          >
            <PlusIcon />
            Add {label.toLowerCase()}
          </Button>
        </>
      )}
    </div>
  )
}

export function StringArrayField(props: FieldProps) {
  return (
    <ArrayField {...props} initial="">
      {(path, index) => <TextField path={path} label={`${props.label} ${index + 1}`} />}
    </ArrayField>
  )
}
