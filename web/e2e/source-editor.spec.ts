import type { Page } from '@playwright/test'
import { findNodeAtLocation, parseTree } from 'jsonc-parser'
import { test, expect } from './fixtures'

const hugeInteger = '900719925474099312345678901234567890'
const longDecimal = '0.123456789012345678901234567890123456789'
const exponent = '1.230000000000000000000000000000001e+123'
const editedDecimal = '0.987654321098765432109876543210987654321'

type SourceDocument = { json: string; revision: string }

function sourceJSON(id: string, url: string) {
  // Deliberately write numeric tokens as text: JSON.stringify on JS numbers would
  // destroy the exact representation before the browser ever sees this fixture.
  return `{
  "version": 1,
  "id": "${id}",
  "name": "Synthetic editor source",
  "adapter": "http_json",
  "url": "${url}",
  "enabled": false,
  "http": {
    "method": "POST",
    "query": {
      "huge": ${hugeInteger},
      "decimal": ${longDecimal},
      "exponent": ${exponent}
    },
    "body": {"nested": [${hugeInteger}, ${longDecimal}, ${exponent}]}
  }
}\n`
}

async function importSource(page: Page, json: string) {
  await page.goto('/providers/new')
  await expect(page.getByRole('button', { name: 'Import JSON', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Use JSON', exact: true }).click()
  await expect(page.getByRole('button', { name: 'JSON', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(page.getByRole('button', { name: 'Import JSON', exact: true })).toBeVisible()
  await page.getByLabel('Import a source JSON definition', { exact: true }).setInputFiles({
    name: 'synthetic-source.json',
    mimeType: 'application/json',
    buffer: Buffer.from(json),
  })
  await page.getByRole('button', { name: 'Replace editor content', exact: true }).click()
  await expect(page.getByRole('button', { name: 'JSON', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Import JSON', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toHaveCount(0)
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(
    'Synthetic editor source',
  )
  await expect(page.getByLabel('Source enabled', { exact: true })).toHaveValue('false')
}

async function exportSource(page: Page): Promise<string> {
  const visual =
    (await page
      .getByRole('button', { name: 'Visual stages', exact: true })
      .getAttribute('aria-pressed')) === 'true'
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const downloaded = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export JSON', exact: true }).click()
  const download = await downloaded
  try {
    const stream = await download.createReadStream()
    if (!stream) throw new Error('The source export did not produce a readable download.')
    const chunks: Buffer[] = []
    for await (const chunk of stream) chunks.push(Buffer.from(chunk))
    return Buffer.concat(chunks).toString('utf8')
  } finally {
    await download.delete()
    if (visual) await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  }
}

async function saveSource(
  page: Page,
  id: string,
  status = 200,
): Promise<SourceDocument | undefined> {
  const response = page.waitForResponse(
    (value) =>
      value.request().method() === 'PUT' &&
      new URL(value.url()).pathname === `/api/providers/${id}`,
  )
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  const saved = await response
  expect(saved.status()).toBe(status)
  if (status !== 200) return
  await expect(page).toHaveURL(new RegExp(`/providers/${id}$`))
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  return (await saved.json()) as SourceDocument
}

async function readSource(page: Page, id: string): Promise<SourceDocument> {
  const response = await page.request.get(`/api/providers/${id}`)
  expect(response.status()).toBe(200)
  return (await response.json()) as SourceDocument
}

async function pasteJSON(page: Page, label: string, text: string) {
  const editor = page.getByRole('textbox', { name: label, exact: true })
  await editor.click()
  await editor.press('ControlOrMeta+A')
  await page.evaluate((value) => navigator.clipboard.writeText(value), text)
  await editor.press('ControlOrMeta+V')
}

test('JSON import, visual edits, export and save preserve exact numeric lexemes', async ({
  page,
  server,
}) => {
  const id = 'precision-source'
  const initial = sourceJSON(id, server.sourceURL)
  await importSource(page, initial)
  expect(await exportSource(page)).toBe(initial)

  await page.getByLabel('Display name', { exact: true }).fill('Precision preserved')
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(page.getByLabel('JSON value for huge', { exact: true })).toHaveValue(hugeInteger)
  await expect(page.getByLabel('JSON value for exponent', { exact: true })).toHaveValue(exponent)
  await page.getByLabel('JSON value for decimal', { exact: true }).fill(editedDecimal)
  await page.getByRole('tab', { name: /^Source fields/ }).click()
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(page.getByLabel('JSON value for decimal', { exact: true })).toHaveValue(
    editedDecimal,
  )

  const exported = await exportSource(page)
  const tree = parseTree(exported)!
  const tokens: [Array<string | number>, string][] = [
    [['http', 'query', 'huge'], hugeInteger],
    [['http', 'query', 'decimal'], editedDecimal],
    [['http', 'query', 'exponent'], exponent],
    [['http', 'body', 'nested', 0], hugeInteger],
    [['http', 'body', 'nested', 1], longDecimal],
    [['http', 'body', 'nested', 2], exponent],
  ]
  for (const [path, token] of tokens) {
    const node = findNodeAtLocation(tree, path)!
    expect(node?.type, `Exported number at ${path.join('.')}`).toBe('number')
    expect(exported.slice(node.offset, node.offset + node.length)).toBe(token)
  }
  const metadata = JSON.parse(exported)
  expect(metadata.name).toBe('Precision preserved')
  expect(metadata.enabled).toBe(false)
  expect(metadata.schedule).toBeUndefined()

  const saved = await saveSource(page, id)
  expect(saved?.json).toBe(exported)
  expect((await readSource(page, id)).json).toBe(exported)
  // Reload from the real provider file/API, not the editor's previous React state.
  await page.reload()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue('Precision preserved')
  expect(await exportSource(page)).toBe(exported)
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(page.getByLabel('JSON value for huge', { exact: true })).toHaveValue(hugeInteger)
  await expect(page.getByLabel('JSON value for decimal', { exact: true })).toHaveValue(
    editedDecimal,
  )
  await expect(page.getByLabel('JSON value for exponent', { exact: true })).toHaveValue(exponent)
})

test('a stale revision retains the local draft until explicit comparison and resolution', async ({
  page,
  server,
}) => {
  const id = 'conflict-source'
  await importSource(page, sourceJSON(id, server.sourceURL))
  await saveSource(page, id)
  const original = await readSource(page, id)
  await page.getByLabel('Display name', { exact: true }).fill('My unsaved local draft')
  const localDraft = await exportSource(page)

  // Simulate another ordinary authenticated writer, with the same API and CSRF
  // requirements as the UI. There are no direct database writes or bypass routes.
  const sessionResponse = await page.request.get('/api/session')
  expect(sessionResponse.status()).toBe(200)
  const session = await sessionResponse.json()
  expect(session.authenticated).toBe(true)
  const remoteJSON = original.json.replace('Synthetic editor source', 'Updated by another writer')
  const update = await page.request.put(`/api/providers/${id}`, {
    headers: { 'X-CSRF-Token': session.csrf_token, Origin: server.origin },
    data: { json: remoteJSON, revision: original.revision },
  })
  expect(update.status()).toBe(200)
  const remote = (await update.json()) as SourceDocument
  expect(remote.revision).not.toBe(original.revision)

  await saveSource(page, id, 409)
  await expect(page.getByRole('heading', { name: 'Revision conflict', exact: true })).toBeVisible()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(
    'My unsaved local draft',
  )
  expect(await exportSource(page)).toBe(localDraft)
  expect((await readSource(page, id)).json).toBe(remoteJSON)

  await page.getByRole('button', { name: 'Compare server version', exact: true }).click()
  await expect(page.getByLabel('Current server JSON version', { exact: true })).toContainText(
    'Updated by another writer',
  )
  expect(await exportSource(page)).toBe(localDraft)
  // Merely looking at the server copy must not rebase or overwrite it.
  await saveSource(page, id, 409)
  expect((await readSource(page, id)).revision).toBe(remote.revision)
  await page.getByRole('button', { name: 'Keep my edits on this revision', exact: true }).click()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(
    'My unsaved local draft',
  )
  const resolved = await saveSource(page, id)
  expect(resolved?.json).toBe(localDraft)
  expect(resolved?.revision).not.toBe(remote.revision)
  await page.reload()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(
    'My unsaved local draft',
  )
  expect((await readSource(page, id)).json).toBe(localDraft)
})

test('invalid raw and structured drafts block saving and stage changes without discarding input', async ({
  page,
  server,
}) => {
  const id = 'invalid-draft-source'
  await importSource(page, sourceJSON(id, server.sourceURL))
  await saveSource(page, id)
  const original = await readSource(page, id)
  await page.getByRole('button', { name: 'JSON', exact: true }).click()
  const rawDraft = '{"version":'
  await pasteJSON(page, 'Source JSON definition', rawDraft)
  await expect(
    page.getByRole('textbox', { name: 'Source JSON definition', exact: true }),
  ).toHaveText(rawDraft)
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await expect(
    page.getByRole('button', { name: 'Check configuration', exact: true }),
  ).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Visual stages', exact: true })).toBeDisabled()
  await page.getByRole('textbox', { name: 'Source JSON definition', exact: true }).press('Tab')
  await expect(
    page.getByRole('textbox', { name: 'Source JSON definition', exact: true }),
  ).toHaveText(rawDraft)
  expect((await readSource(page, id)).revision).toBe(original.revision)

  await pasteJSON(page, 'Source JSON definition', original.json)
  await page.getByRole('button', { name: 'Visual stages', exact: true }).click()
  await page.getByRole('tab', { name: /^Connection/ }).click()
  const fieldDraft = '{"pending":'
  await pasteJSON(page, 'Request body · JSON', fieldDraft)
  const body = page.getByRole('textbox', { name: 'Request body · JSON', exact: true })
  await expect(body).toHaveText(fieldDraft)
  for (const name of ['Save', 'Check configuration', 'JSON', 'Visual stages']) {
    await expect(page.getByRole('button', { name, exact: true })).toBeDisabled()
  }
  await expect(page.getByRole('button', { name: 'Export JSON', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Import JSON', exact: true })).toHaveCount(0)
  for (const name of [/^Identity/, /^Pagination/, /^Source fields/, /^Imports/]) {
    await expect(page.getByRole('tab', { name })).toBeDisabled()
  }
  const connection = page.getByRole('tab', { name: /^Connection/ })
  await connection.press('ArrowDown')
  await expect(connection).toHaveAttribute('aria-selected', 'true')
  await expect(body).toHaveText(fieldDraft)
  expect((await readSource(page, id)).json).toBe(original.json)

  await page.getByRole('button', { name: 'Discard field draft', exact: true }).click()
  expect(await exportSource(page)).toBe(original.json)
  await expect(page.getByRole('tab', { name: /^Source fields/ })).toBeEnabled()

  // Repairing, as well as explicitly discarding, must release the navigation/save
  // guard and preserve the newly entered exact number through a stage unmount.
  await pasteJSON(page, 'Request body · JSON', fieldDraft)
  const repairedBody = `{"new_precision": ${hugeInteger}}`
  await pasteJSON(page, 'Request body · JSON', repairedBody)
  await page.getByRole('tab', { name: /^Source fields/ }).click()
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await expect(body).toHaveText(repairedBody)
  const repaired = await exportSource(page)
  await saveSource(page, id)
  expect((await readSource(page, id)).json).toBe(repaired)
  expect(repaired).toContain(hugeInteger)
})

test('blank numeric rows stay editable without deleting or shifting sibling values', async ({
  page,
  server,
}) => {
  const id = 'blank-rows-source'
  const initial = sourceJSON(id, server.sourceURL).replace(
    '"enabled": false,',
    '"enabled": false,\n  "search": {"categories": [2000, 5000]},',
  )
  await importSource(page, initial)
  await saveSource(page, id)
  await page.getByRole('tab', { name: /^Connection/ }).click()
  await page.getByText('Headers & search', { exact: true }).click()
  await page.getByLabel('Category 1', { exact: true }).fill('')
  await expect(page.getByLabel('Category 1', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Category 2', { exact: true })).toHaveValue('5000')
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByLabel('Category 1', { exact: true }).fill('3000')
  await page.getByLabel('JSON value for huge', { exact: true }).fill('')
  await expect(page.getByLabel('JSON value for huge', { exact: true })).toHaveValue('')
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
  await page.getByLabel('JSON value for huge', { exact: true }).fill(hugeInteger)
  const saved = await saveSource(page, id)
  expect(JSON.parse(saved!.json).search.categories).toEqual([3000, 5000])
  const node = findNodeAtLocation(parseTree(saved!.json)!, ['http', 'query', 'huge'])!
  expect(saved!.json.slice(node.offset, node.offset + node.length)).toBe(hugeInteger)
})

test('creating an existing ID cannot adopt its revision and overwrite the existing source', async ({
  page,
  server,
}) => {
  const id = 'duplicate-create-source'
  const initial = sourceJSON(id, server.sourceURL)
  await importSource(page, initial)
  await saveSource(page, id)
  const original = await readSource(page, id)
  await importSource(page, initial)
  await page.getByLabel('Display name', { exact: true }).fill('A separate source')
  await saveSource(page, id, 409)
  await expect(
    page.getByRole('button', { name: 'Compare server version', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.getByRole('button', { name: 'Keep my edits on this revision', exact: true }),
  ).toHaveCount(0)
  expect(await readSource(page, id)).toEqual(original)
  const newID = 'duplicate-create-repaired'
  await page.getByLabel('Source identifier', { exact: true }).fill(newID)
  await saveSource(page, newID)
  expect(JSON.parse((await readSource(page, newID)).json).name).toBe('A separate source')
  expect(await readSource(page, id)).toEqual(original)
})
