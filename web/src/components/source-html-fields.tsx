import { useId, useState } from 'react'
import { PlusIcon, TrashIcon } from '@phosphor-icons/react'
import type { ProviderPath } from '@/lib/provider-json'
import { ConfirmAction } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  AdvancedSection,
  BooleanField,
  FieldGrid,
  FieldSection,
  JSONField,
  SelectField,
  TextField,
  useSourceEditor,
} from '@/components/source-editor-fields'

const fieldNames: Record<string, string> = {
  title: 'Title',
  info_hash: 'Info hash',
  size: 'Size',
  published_at: 'Publication date',
  categories: 'Categories',
  imdb_id: 'IMDb ID',
  tmdb_id: 'TMDB ID',
  tvdb_id: 'TVDB ID',
  season: 'Season',
  episode: 'Episode',
  languages: 'Languages',
}

export function SourceHTMLCompatibility() {
  const { document, change } = useSourceEditor()
  const adapter = document.get('adapter')
  const paths: ProviderPath[] = []
  if (adapter === 'html') {
    if (document.getIn(['mapping', 'id']) || document.keysIn(['mapping', 'fields']).length)
      paths.push(['mapping'])
    if (document.kindIn(['traversal']) !== undefined) paths.push(['traversal'])
    if (document.getIn(['http', 'items_path'])) paths.push(['http', 'items_path'])
    if (document.getIn(['http', 'catalog']) === true) paths.push(['http', 'catalog'])
    for (const key of ['next_path', 'total_path', 'current_path', 'cursor_param'])
      if (document.getIn(['pagination', key])) paths.push(['pagination', key])
    if (document.getIn(['pagination', 'type']) === 'cursor') paths.push(['pagination', 'type'])
    if (document.getIn(['search', 'query']) || document.lengthIn(['search', 'categories']))
      paths.push(['search'])
    for (const key of document.keysIn(['options']))
      if (!['published_at_unit', 'local_categories'].includes(key)) paths.push(['options', key])
  } else if (document.kindIn(['html']) !== undefined) {
    paths.push(['html'])
    if (adapter === 'torznab' || document.getIn(['pagination', 'type']) === 'next')
      paths.push(['pagination'])
  }
  if (!paths.length) return null
  return (
    <div className="notice space-y-3">
      <p>
        Settings for another source type remain in this draft. Review the JSON or clear them before
        saving.
      </p>
      <ConfirmAction
        trigger={
          <Button variant="warning" size="sm">
            Clear incompatible source settings
          </Button>
        }
        title="Clear incompatible source settings?"
        description={`Removes ${paths.map((path) => path.join('.')).join(', ')} from this draft. Stored secrets and published records are unchanged.`}
        label="Clear settings"
        variant="warning"
        action={async () => change((next) => paths.forEach((path) => next.deleteIn(path)))}
      />
    </div>
  )
}

function HTMLFieldSettings({
  path,
  label,
  identity = false,
  link = false,
  count = false,
}: {
  path: ProviderPath
  label: string
  identity?: boolean
  link?: boolean
  count?: boolean
}) {
  const { document } = useSourceEditor()
  const hash = path.at(-1) === 'info_hash'
  const title = path.at(-1) === 'title'
  const size = path.at(-1) === 'size'
  const date = path.at(-1) === 'published_at'
  const format = document.getIn([...path, 'format'])
  return (
    <div className="min-w-0 space-y-4">
      <FieldGrid>
        <TextField
          path={[...path, 'selector']}
          label={`${label} selector`}
          placeholder={
            identity
              ? 'Leave empty to read this row'
              : hash
                ? "a[href^='magnet:']"
                : size
                  ? '.size'
                  : date
                    ? 'time[datetime]'
                    : '.title'
          }
          help={identity || link ? 'Empty reads the current row itself.' : undefined}
        />
        <TextField
          path={[...path, 'attribute']}
          label={`${label} attribute`}
          placeholder={identity || link || hash ? 'href' : 'Leave empty for visible text'}
          help={
            identity || link
              ? 'Empty reads text; href reads a link; data-id reads that attribute.'
              : undefined
          }
        />
      </FieldGrid>
      {!link && !title && (
        <SelectField
          path={[...path, 'format']}
          label={`${label} format`}
          fallback={count ? 'Whole number' : 'Text (default)'}
          choices={
            identity
              ? ['text', 'integer', 'url']
              : title
                ? ['text']
                : hash
                  ? ['info_hash', 'torrent', 'text']
                  : size
                    ? ['size', 'integer', 'text']
                    : count
                      ? ['integer']
                      : [
                          'text',
                          'integer',
                          'size',
                          'date',
                          'url',
                          'info_hash',
                          ...(hash ? ['torrent'] : []),
                        ]
          }
          labels={{
            text: 'Text',
            integer: 'Whole number',
            size: 'File size · bytes, MB, GiB…',
            date: 'Date',
            url: 'Absolute link',
            info_hash: 'Info hash or magnet link',
            torrent: 'Read hash from a torrent file',
          }}
          help={
            hash
              ? 'Magnet links retain only the hash. Torrent-file links read metadata only, using the source connection.'
              : undefined
          }
        />
      )}
      <AdvancedSection
        title={`${label} extraction options`}
        configuredPaths={[
          [...path, 'pattern'],
          [...path, 'date_layout'],
          [...path, 'timezone'],
        ]}
      >
        {format === 'date' && (
          <FieldGrid>
            <TextField
              path={[...path, 'date_layout']}
              label={`${label} date layout`}
              placeholder="2006-01-02 15:04:05"
              help="Leave empty for ISO / email dates. Custom formats use Go’s reference date shown above."
            />
            <TextField
              path={[...path, 'timezone']}
              label={`${label} timezone`}
              placeholder="UTC"
              help="Used when the displayed date has no timezone, for example Europe/Paris."
            />
          </FieldGrid>
        )}
        <TextField
          path={[...path, 'pattern']}
          label={`${label} extraction pattern`}
          placeholder={identity ? '/items/([0-9]+)' : '([0-9]+)'}
          help="Optional regular expression. Its single capture, or the complete match without captures, becomes the value."
        />
        {!identity && !link && !count && (
          <>
            {!title && (
              <BooleanField
                path={[...path, 'required']}
                label={`${label} required`}
                help="A missing value stops the page. Optional fields are omitted when no value matches."
              />
            )}
            {!hash && !title && !size && !date && (
              <BooleanField
                path={[...path, 'all']}
                label={`${label} multiple values`}
                help="Collect all matching values, for example category IDs or languages."
              />
            )}
          </>
        )}
      </AdvancedSection>
    </div>
  )
}

function HTMLFieldMap({ path, detail = false }: { path: ProviderPath; detail?: boolean }) {
  const { document, change } = useSourceEditor()
  const [name, setName] = useState('')
  const id = useId()
  const keys = document.keysIn(path)
  const valid =
    keys.length < 64 && /^[A-Za-z][A-Za-z0-9_.-]{0,127}$/.test(name) && !keys.includes(name)
  return (
    <div className="min-w-0 space-y-5">
      {keys.map((key) => {
        const label = `${detail ? 'Detail ' : ''}${fieldNames[key] || key}`
        return (
          <div key={key} className="min-w-0 space-y-4 rounded-md border border-border p-4">
            <div className="flex items-center justify-between gap-3">
              <h4 className="min-w-0 break-words font-medium">{label}</h4>
              <Button
                variant="warning"
                size="icon-sm"
                aria-label={`Remove ${label} mapping`}
                onClick={() => change((next) => next.deleteIn([...path, key]))}
              >
                <TrashIcon />
              </Button>
            </div>
            <HTMLFieldSettings path={[...path, key]} label={label} />
          </div>
        )
      })}
      <div className="flex flex-wrap items-end gap-3">
        <div className="field min-w-0 flex-1">
          <Label htmlFor={id}>New {detail ? 'detail ' : ''}field</Label>
          <Input
            id={id}
            value={name}
            list={`${id}-names`}
            placeholder="Choose or enter a field name"
            onChange={(event) => setName(event.target.value.trim())}
          />
          <datalist id={`${id}-names`}>
            {Object.keys(fieldNames)
              .filter((key) => !keys.includes(key))
              .map((key) => (
                <option key={key} value={key}>
                  {fieldNames[key]}
                </option>
              ))}
          </datalist>
        </div>
        <Button
          variant="outline"
          disabled={!valid}
          onClick={() => {
            if (!valid) return
            change((next) => next.setIn([...path, name], { selector: '' }))
            setName('')
          }}
        >
          <PlusIcon /> Add field
        </Button>
      </div>
    </div>
  )
}

export function HTMLMappingSettings() {
  const { document, change } = useSourceEditor()
  const details = document.kindIn(['html', 'details']) !== undefined
  return (
    <>
      <FieldSection title="Listing structure">
        <FieldGrid>
          <TextField
            path={['html', 'page_selector']}
            label="Listing container selector"
            placeholder="#catalog"
            help="One container unique to the real listing. Login and error pages must not match it."
          />
          <TextField
            path={['html', 'rows_selector']}
            label="Record selector"
            placeholder="tbody > tr[data-id]"
            help="One row per torrent, relative to the listing container. Exclude headings and advertisements."
          />
        </FieldGrid>
      </FieldSection>
      <FieldSection title="Native identity">
        <p className="help">A stable site ID keeps imports and updates tied to the same record.</p>
        <HTMLFieldSettings path={['html', 'id']} label="Native ID" identity />
      </FieldSection>
      <FieldSection
        title="Field mappings"
        description="Selectors read each row. Leave the attribute empty for text, or use href, datetime or a data attribute. Titles are required; optional fields can be omitted."
      >
        <HTMLFieldMap path={['html', 'fields']} />
      </FieldSection>
      <AdvancedSection
        title="Detail pages"
        configuredPaths={[['html', 'details']]}
        description="Fetch missing fields from each record’s detail page. Existing listing values are kept. These extra requests use the same authentication, proxy and request limits."
      >
        {details ? (
          <>
            <HTMLFieldSettings path={['html', 'details', 'url']} label="Detail link" link />
            <TextField
              path={['html', 'details', 'page_selector']}
              label="Detail container selector"
              placeholder="#item-details"
              help="One container unique to a valid detail page. Its field selectors are relative to this container."
            />
            <HTMLFieldMap path={['html', 'details', 'fields']} detail />
            <ConfirmAction
              trigger={<Button variant="warning">Remove detail settings</Button>}
              label="Remove detail settings"
              title="Remove detail settings?"
              description="Removes these selectors from the draft. Saved source data is unchanged."
              action={async () => change((next) => next.deleteIn(['html', 'details']))}
              variant="warning"
            />
          </>
        ) : (
          <Button
            variant="outline"
            onClick={() =>
              change((next) =>
                next.setIn(['html', 'details'], {
                  url: { selector: 'a.details', attribute: 'href' },
                  page_selector: '#item-details',
                  fields: {
                    info_hash: {
                      selector: "a[href^='magnet:']",
                      attribute: 'href',
                      format: 'info_hash',
                      required: true,
                    },
                  },
                }),
              )
            }
          >
            <PlusIcon /> Configure detail pages
          </Button>
        )}
      </AdvancedSection>
    </>
  )
}

export function HTMLPaginationSettings() {
  const { document, change } = useSourceEditor()
  const strategy = document.getIn(['pagination', 'type']) || 'next'
  return (
    <>
      <FieldSection title="Page strategy">
        <SelectField
          path={['pagination', 'type']}
          label="Pagination type"
          fallback="Follow next-page links (default)"
          choices={['next', 'page', 'offset', 'none']}
          labels={{
            next: 'Follow next-page links',
            page: 'Page number',
            offset: 'Record offset',
            none: 'Single page',
          }}
        />
        {strategy !== 'none' && (
          <TextField
            path={['html', 'next_selector']}
            label="Next-page link selector"
            placeholder="a[rel=next]:not([aria-disabled=true])"
            help="Select enabled next links. No matching link ends the listing. Required for next-link pagination; optional for numbered pages."
          />
        )}
        {(strategy === 'page' || strategy === 'offset') && (
          <FieldGrid>
            <TextField
              path={['pagination', strategy === 'page' ? 'page_param' : 'offset_param']}
              label={strategy === 'page' ? 'Page parameter' : 'Offset parameter'}
              placeholder={strategy === 'page' ? 'page' : 'offset'}
            />
            <JSONField
              path={['pagination', 'start']}
              label="Starting page or offset"
              integer
              placeholder={strategy === 'page' ? '1' : '0'}
            />
          </FieldGrid>
        )}
      </FieldSection>
      <FieldSection title="Empty results">
        <TextField
          path={['html', 'empty_selector']}
          label="Empty-result selector"
          placeholder=".empty-results"
          help="A marker shown only when no torrents exist. Without a matching marker, a page with no records fails safely instead of replacing your catalog."
        />
      </FieldSection>
      <AdvancedSection title="Request parameters">
        <FieldGrid>
          <JSONField path={['page_size']} label="Page size" integer placeholder="100" />
          <TextField
            path={['pagination', 'size_param']}
            label="Page-size parameter"
            placeholder="Leave empty for the website’s fixed size"
          />
        </FieldGrid>
        {strategy === 'page' || strategy === 'offset' ? (
          <SelectField
            path={['pagination', 'in']}
            label="Parameter location"
            choices={['query', 'body']}
            fallback="URL query (default)"
            labels={{ query: 'URL query', body: 'POST form' }}
            help="Form parameters require HTTP method POST."
          />
        ) : null}
      </AdvancedSection>
      <AdvancedSection
        title="Response checks"
        configuredPaths={[
          ['html', 'total'],
          ['html', 'current'],
        ]}
      >
        {(['total', 'current'] as const).map((key) => {
          const label = key === 'total' ? 'Total records' : 'Current position'
          return document.kindIn(['html', key]) !== undefined ? (
            <div key={key} className="space-y-4">
              <h4 className="font-medium">{label}</h4>
              <HTMLFieldSettings path={['html', key]} label={label} count />
              <Button
                variant="warning"
                size="sm"
                onClick={() => change((next) => next.deleteIn(['html', key]))}
              >
                Remove {label.toLowerCase()} check
              </Button>
            </div>
          ) : (
            <Button
              key={key}
              variant="outline"
              size="sm"
              onClick={() =>
                change((next) => next.setIn(['html', key], { selector: '', format: 'integer' }))
              }
            >
              <PlusIcon /> Add {label.toLowerCase()} check
            </Button>
          )
        })}
      </AdvancedSection>
    </>
  )
}
