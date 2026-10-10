import {
  applyEdits,
  findNodeAtLocation,
  getNodeValue,
  modify,
  parseTree,
  printParseErrorCode,
  type Node,
  type ParseError,
} from 'jsonc-parser'

export type ProviderPath = (string | number)[]
export interface ProviderJSON {
  readonly errors: string[]
  get(key: string): unknown
  getIn(path: ProviderPath): unknown
  set(key: string, value: unknown): void
  setIn(path: ProviderPath, value: unknown): void
  delete(key: string): void
  deleteIn(path: ProviderPath): void
  rawIn(path: ProviderPath): string
  kindIn(path: ProviderPath): Node['type'] | undefined
  keysIn(path: ProviderPath): string[]
  lengthIn(path: ProviderPath): number
  setRawIn(path: ProviderPath, value: string): void
  toString(): string
}
const strictOptions = {
  disallowComments: true,
  allowTrailingComma: false,
  allowEmptyContent: false,
}
const editOptions = { formattingOptions: { insertSpaces: true, tabSize: 2, eol: '\n' } }

function inspect(source: string, objectRoot: boolean) {
  const parseErrors: ParseError[] = []
  const root = parseTree(source, parseErrors, strictOptions)
  const errors = parseErrors.map(({ error, offset }) => {
    const before = source.slice(0, offset)
    const line = before.split('\n').length
    const column = offset - before.lastIndexOf('\n')
    return `${printParseErrorCode(error)} at line ${line}, column ${column}.`
  })
  if (!root || (objectRoot && root.type !== 'object'))
    errors.push(objectRoot ? 'The source definition must be a JSON object.' : 'Enter a JSON value.')
  function duplicates(node: Node) {
    if (node.type === 'object') {
      const seen = new Set<string>()
      for (const property of node.children ?? []) {
        const key = property.children?.[0]
        if (!key) continue
        if (seen.has(key.value)) {
          const line = source.slice(0, key.offset).split('\n').length
          errors.push(`Duplicate property at line ${line}.`)
        }
        seen.add(key.value)
      }
    }
    for (const child of node.children ?? []) duplicates(child)
  }
  if (root) duplicates(root)
  return { root, errors }
}

export function validateJSONValue(source: string): string[] {
  return inspect(source, false).errors
}

// Reads may evaluate numbers for display; edits never serialize an existing subtree.
// Use rawIn/setRawIn when moving or editing arbitrary JSON values.
export function parseProviderJSON(source: string): ProviderJSON {
  let text = source
  let state = inspect(text, true)
  const nodeAt = (path: ProviderPath) => state.root && findNodeAtLocation(state.root, path)
  function assertEditable() {
    if (state.errors.length) throw new Error('Repair the JSON before using structured controls.')
  }
  function commit(next: string) {
    const checked = inspect(next, true)
    if (checked.errors.length) throw new Error(checked.errors[0])
    text = next
    state = checked
  }
  function prepareParents(path: ProviderPath) {
    let next = text
    let root = state.root
    for (let index = 1; index < path.length; index++) {
      const parentPath = path.slice(0, index)
      const parent = root && findNodeAtLocation(root, parentPath)
      if (parent?.type === 'null') {
        next = applyEdits(
          next,
          modify(next, parentPath, typeof path[index] === 'number' ? [] : {}, editOptions),
        )
        root = inspect(next, true).root
      }
    }
    return next
  }
  function setIn(path: ProviderPath, value: unknown) {
    assertEditable()
    if (!path.length && (value === null || Array.isArray(value) || typeof value !== 'object'))
      throw new Error('The source definition must remain a JSON object.')
    const next = prepareParents(path)
    commit(applyEdits(next, modify(next, path, value, editOptions)))
  }
  function setRawIn(path: ProviderPath, value: string) {
    assertEditable()
    const checked = inspect(value, path.length === 0)
    if (checked.errors.length) throw new Error(checked.errors[0])
    let next = prepareParents(path)
    let target = nodeAt(path)
    if (!target) {
      next = applyEdits(next, modify(next, path, null, editOptions))
      const root = inspect(next, true).root
      target = root && findNodeAtLocation(root, path)
    }
    if (!target) throw new Error('This JSON value cannot be edited at that location.')
    commit(
      applyEdits(next, [{ offset: target.offset, length: target.length, content: value.trim() }]),
    )
  }
  function deleteIn(path: ProviderPath) {
    assertEditable()
    if (!path.length) throw new Error('The source definition must remain a JSON object.')
    if (nodeAt(path)) commit(applyEdits(text, modify(text, path, undefined, editOptions)))
  }
  return {
    get errors() {
      return state.errors
    },
    get(key: string): unknown {
      return this.getIn([key])
    },
    getIn(path: ProviderPath): unknown {
      const node = nodeAt(path)
      return node ? getNodeValue(node) : undefined
    },
    set(key: string, value: unknown) {
      setIn([key], value)
    },
    setIn,
    delete(key: string) {
      deleteIn([key])
    },
    deleteIn,
    rawIn(path: ProviderPath): string {
      const node = nodeAt(path)
      return node ? text.slice(node.offset, node.offset + node.length) : ''
    },
    kindIn(path: ProviderPath) {
      return nodeAt(path)?.type
    },
    keysIn(path: ProviderPath): string[] {
      const node = nodeAt(path)
      return node?.type === 'object'
        ? (node.children ?? []).map((property) => String(property.children?.[0]?.value))
        : []
    },
    lengthIn(path: ProviderPath): number {
      const node = nodeAt(path)
      return node?.type === 'array' ? (node.children?.length ?? 0) : 0
    },
    setRawIn,
    toString(): string {
      return text
    },
  }
}

// Three-way field merging keeps untouched server changes and exact numeric
// tokens. Arrays are atomic: their positions are not stable source identities.
export function rebaseProviderJSON(baseText: string, localText: string, serverText: string) {
  const base = parseProviderJSON(baseText),
    local = parseProviderJSON(localText),
    server = parseProviderJSON(serverText)
  if ([base, local, server].some((document) => document.errors.length))
    throw new Error('Repair the JSON before merging source versions.')
  function same(a: ProviderJSON, b: ProviderJSON, path: ProviderPath): boolean {
    const kind = a.kindIn(path)
    if (kind !== b.kindIn(path)) return false
    if (kind === 'object') {
      const keys = a.keysIn(path)
      return (
        keys.length === b.keysIn(path).length && keys.every((key) => same(a, b, [...path, key]))
      )
    }
    if (kind === 'array')
      return (
        a.lengthIn(path) === b.lengthIn(path) &&
        Array.from({ length: a.lengthIn(path) }, (_, i) => i).every((i) => same(a, b, [...path, i]))
      )
    return kind === 'number' ? a.rawIn(path) === b.rawIn(path) : a.getIn(path) === b.getIn(path)
  }
  const conflicts: ProviderPath[] = []
  function merge(path: ProviderPath) {
    if (same(base, local, path) || same(local, server, path)) return
    if (same(base, server, path)) {
      const value = local.rawIn(path)
      value ? server.setRawIn(path, value) : server.deleteIn(path)
      return
    }
    if (
      (base.kindIn(path) === 'object' || base.kindIn(path) === undefined) &&
      local.kindIn(path) === 'object' &&
      server.kindIn(path) === 'object'
    ) {
      for (const key of new Set([
        ...base.keysIn(path),
        ...local.keysIn(path),
        ...server.keysIn(path),
      ]))
        merge([...path, key])
    } else conflicts.push(path)
  }
  merge([])
  return { json: server.toString(), conflicts }
}
