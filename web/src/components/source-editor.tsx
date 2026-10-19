import { useCallback, useEffect, useId, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  FingerprintIcon,
  GlobeIcon,
  RowsIcon,
  ArrowsLeftRightIcon,
  TimerIcon,
  BracketsCurlyIcon,
  FlowArrowIcon,
  CalendarIcon,
} from '@phosphor-icons/react'
import { parseProviderJSON, type ProviderJSON } from '@/lib/provider-json'
import { Button } from '@/components/ui/button'
import { ConfirmAction } from '@/components/common'
import { SecretPicker } from '@/components/secret-picker'
import { DurationSelect, ScheduleTimingFields } from '@/components/schedule-fields'
import { timingMode, type TimingMode } from '@/lib/schedule-input'
import { Switch } from '@/components/ui/switch'
import { isJSONAdapter } from '@/lib/source-adapters'
import { Label } from '@/components/ui/label'
import { CodeEditor } from '@/components/code-editor'
import {
  HTMLMappingSettings,
  HTMLPaginationSettings,
  SourceHTMLCompatibility,
} from '@/components/source-html-fields'
import {
  AdvancedSection,
  ArrayField,
  BooleanField,
  FieldGrid,
  FieldSection,
  JSONField,
  MapField,
  SelectField,
  SourceEditorContext,
  StringArrayField,
  TextField,
  useSourceEditor,
} from '@/components/source-editor-fields'

const stages = [
  {
    id: 'identity',
    label: 'Identity',
    icon: FingerprintIcon,
    description: 'Name the source and select its adapter.',
  },
  {
    id: 'connection',
    label: 'Connection',
    icon: GlobeIcon,
    description: 'Define the endpoint, authentication and requests.',
  },
  {
    id: 'pagination',
    label: 'Pagination',
    icon: RowsIcon,
    description: 'Control how the collector advances through a listing.',
  },
  {
    id: 'mapping',
    label: 'Source fields',
    icon: ArrowsLeftRightIcon,
    description:
      'Read torrent fields from this source. This is separate from film & series identification.',
  },
  {
    id: 'collection',
    label: 'Imports',
    icon: TimerIcon,
    description: 'Control request pacing and collection limits.',
  },
  {
    id: 'schedules',
    label: 'Schedules',
    icon: CalendarIcon,
    description: 'Choose when this source imports automatically.',
  },
] as const

const remoteStages = stages.filter((stage) => stage.id !== 'schedules')

function IdentitySettings({ sourceID }: { sourceID?: string }) {
  const { document } = useSourceEditor()
  const jsonAdapter = document.get('adapter') === 'http_json' ? 'http_json' : 'json'
  return (
    <FieldSection title="Source identity">
      <FieldGrid>
        <TextField path={['name']} label="Display name" placeholder="Archive name" />
        <TextField
          path={['id']}
          label="Source identifier"
          placeholder="archive-main"
          help={
            sourceID
              ? 'Keep this identifier stable. Invalid definitions can be repaired before saving.'
              : 'Use a unique, safe identifier such as archive-main.'
          }
        />
        <SelectField
          path={['adapter']}
          label="Source type"
          choices={['torznab', 'html', jsonAdapter]}
          labels={{ json: 'JSON', http_json: 'JSON', torznab: 'Torznab', html: 'HTML website' }}
        />
        <BooleanField
          path={['enabled']}
          label="Source enabled"
          required
          help="Allows manual and scheduled runs. Saving does not start collection."
        />
      </FieldGrid>
      <SourceHTMLCompatibility />
      <AdvancedSection title="Definition format">
        <JSONField
          path={['version']}
          label="Definition version"
          integer
          help="Current definition version: 1."
        />
      </AdvancedSection>
    </FieldSection>
  )
}

function SourceSecretField({ path, label }: { path: string[]; label: string }) {
  const { document, change, readOnly } = useSourceEditor()
  const current = document.getIn(path)
  return (
    <SecretPicker
      label={label}
      value={typeof current === 'string' ? current : ''}
      prefix="source_auth"
      addLabel={`Add ${label.replace(/ reference$/i, '').toLowerCase()}`}
      valueLabel={
        label === 'Secret reference' ? 'Credential value' : label.replace(/ reference$/i, '')
      }
      disabled={readOnly}
      onChange={(reference) =>
        change((next) => (reference ? next.setIn(path, reference) : next.deleteIn(path)))
      }
    />
  )
}

function AuthenticationSettings() {
  const { document, change } = useSourceEditor()
  const type = document.getIn(['auth', 'type']) || 'none'
  const fields = [
    { key: 'secret_ref', label: 'Secret reference', active: type !== 'basic' && type !== 'none' },
    { key: 'username_ref', label: 'Username reference', active: type === 'basic' },
    { key: 'password_ref', label: 'Password reference', active: type === 'basic' },
  ]
  const placement = (
    <SelectField
      path={['auth', 'in']}
      label="Credential placement"
      choices={['header', 'query', 'bearer', 'cookie']}
      help="API keys require header or query. Keep other placement settings only when required by your source."
    />
  )
  const name = (
    <TextField
      path={['auth', 'name']}
      label="Credential parameter name"
      help="Parameter or header name expected by this source."
    />
  )
  const named = type === 'query' || type === 'header' || type === 'api_key'
  const incompatible = fields
    .filter((field) => !field.active && document.getIn(['auth', field.key]))
    .map((field) => field.key)
  const placementValue = document.getIn(['auth', 'in'])
  if (
    placementValue &&
    (type === 'none' || type === 'basic' || (type !== 'api_key' && placementValue !== type))
  )
    incompatible.push('in')
  if (!named && document.getIn(['auth', 'name'])) incompatible.push('name')
  return (
    <FieldSection
      title="Authentication"
      description="Choose how this source authenticates, then select or add its credentials. Values are encrypted in Secrets; only references are saved in the source definition."
    >
      <FieldGrid>
        <SelectField
          path={['auth', 'type']}
          label="Authentication type"
          choices={['none', 'basic', 'bearer', 'cookie', 'query', 'header', 'api_key']}
          labels={{
            none: 'No authentication',
            basic: 'Username & password',
            bearer: 'Bearer token',
            cookie: 'Cookie',
            query: 'URL parameter',
            header: 'HTTP header',
            api_key: 'API key',
          }}
          fallback="No authentication (default)"
        />
        {fields
          .filter((field) => field.active)
          .map((field) => (
            <SourceSecretField key={field.key} path={['auth', field.key]} label={field.label} />
          ))}
        {type === 'api_key' && placement}
        {named && name}
      </FieldGrid>
      {incompatible.length > 0 && (
        <div className="notice space-y-3">
          <p>
            Settings from another authentication type remain in this draft. Clear incompatible
            fields explicitly, or edit them in Other authentication settings before saving.
          </p>
          <ConfirmAction
            title="Clear incompatible authentication fields?"
            description="Only incompatible authentication fields are removed from this draft. Credentials remain in Secrets. Save the source to apply the change."
            label="Clear incompatible fields"
            variant="warning"
            trigger={
              <Button type="button" variant="warning">
                Clear incompatible fields
              </Button>
            }
            action={async () =>
              change((next) => {
                for (const key of incompatible) next.deleteIn(['auth', key])
              })
            }
          />
        </div>
      )}
      <AdvancedSection
        title="Other authentication settings"
        description="Settings for other authentication types are retained. Editing the type never clears these values."
      >
        <FieldGrid>
          {fields
            .filter((field) => !field.active)
            .map((field) => (
              <SourceSecretField key={field.key} path={['auth', field.key]} label={field.label} />
            ))}
          {type !== 'api_key' && placement}
          {!named && name}
        </FieldGrid>
      </AdvancedSection>
    </FieldSection>
  )
}

function ConnectionSettings() {
  const { document } = useSourceEditor()
  const torznab = document.get('adapter') === 'torznab'
  const html = document.get('adapter') === 'html'
  return (
    <>
      <FieldSection
        title="Endpoint"
        description="Use credential-free URLs. Secrets are resolved from the vault when a run starts."
      >
        <TextField
          path={['url']}
          label="Source URL"
          placeholder={
            html ? 'https://archive.example/catalog' : 'https://archive.example/api/items'
          }
        />
        {!torznab && (
          <SelectField
            path={['http', 'method']}
            label="HTTP method"
            choices={['GET', 'POST']}
            fallback="Default · GET"
          />
        )}
        {!torznab && (
          <AdvancedSection
            title="Additional listings"
            configuredPaths={[['http', 'listing_paths']]}
          >
            <StringArrayField
              path={['http', 'listing_paths']}
              label="Additional listing paths"
              help="Paths on the same source, such as /catalog/audio. Imported after Source URL with the same fields and request settings. Each listing keeps its own pagination and update boundary."
            />
          </AdvancedSection>
        )}
      </FieldSection>
      <AuthenticationSettings />
      {!torznab && (
        <FieldSection title="Request parameters">
          <MapField
            path={['http', 'query']}
            label="Base query · Full and all modes"
            help="Shared filters and Full ordering. Values are strict JSON, including strings in quotes."
          />
          <AdvancedSection
            title="Incremental ordering"
            configuredPaths={[['http', 'incremental_query']]}
          >
            <MapField
              path={['http', 'incremental_query']}
              label="Incremental query overrides"
              help="Applied only during Incremental collection. Configure newest-first ordering independently from Full here."
            />
          </AdvancedSection>
          {document.getIn(['http', 'method']) === 'POST' ? (
            <JSONField
              path={['http', 'body']}
              label={html ? 'Form values · JSON object' : 'Request body · JSON'}
              help={
                html
                  ? 'Form field names and values, for example {"search": "", "sort": "newest"}. Sent as an HTML form, not JSON.'
                  : 'Optional POST body. Leave empty to omit. Arbitrary JSON and numeric precision are preserved.'
              }
              multiline
            />
          ) : (
            <AdvancedSection title="Request body">
              <JSONField
                path={['http', 'body']}
                label="Request body · JSON"
                multiline
                help="Used for POST requests. Existing values are retained when the HTTP method changes."
              />
            </AdvancedSection>
          )}
        </FieldSection>
      )}
      <AdvancedSection
        title={html ? 'Headers' : 'Headers & search'}
        configuredPaths={[
          ['http', 'headers'],
          ['http', 'secret_headers'],
          ...(!torznab && !html ? [['search']] : []),
        ]}
        description="Ordinary headers contain non-secret values. Secret headers map a header name to a vault reference."
      >
        <MapField path={['http', 'headers']} label="HTTP headers" strings />
        <MapField path={['http', 'secret_headers']} label="Secret header references" strings />
        {!torznab && !html && <TextField path={['search', 'query']} label="Search query" />}
        {!torznab && !html && (
          <ArrayField path={['search', 'categories']} label="Categories" initial={0}>
            {(path, index) => <JSONField path={path} label={`Category ${index + 1}`} integer />}
          </ArrayField>
        )}
      </AdvancedSection>
      {torznab && (
        <FieldSection title="Search scope">
          <FieldGrid>
            <SelectField
              path={['options', 'search_mode']}
              label="Search mode"
              choices={['search', 'tvsearch', 'movie', 'music', 'book']}
              fallback="General search (default)"
              labels={{
                search: 'General search',
                tvsearch: 'Series',
                movie: 'Films',
                music: 'Music',
                book: 'Books',
              }}
            />
            <SelectField
              path={['options', 'category_scope']}
              label="Category requests"
              choices={['combined', 'each', 'advertised']}
              fallback="Combined (default)"
              labels={{
                combined: 'Combine selected categories',
                each: 'Import each category separately',
                advertised: 'Use categories advertised by the source',
              }}
              help="Use separate requests when the source limits each category independently."
            />
          </FieldGrid>
          <TextField
            path={['search', 'query']}
            label="Search query"
            help="Optional title filter sent to this source."
          />
          <ArrayField
            path={['search', 'categories']}
            label="Categories"
            initial={0}
            help="Numeric category identifiers, especially for Torznab."
          >
            {(path, index) => <JSONField path={path} label={`Category ${index + 1}`} integer />}
          </ArrayField>
        </FieldSection>
      )}
      {!html && (
        <AdvancedSection title="Adapter compatibility">
          {torznab && (
            <SelectField
              path={['http', 'method']}
              label="HTTP method"
              choices={['GET', 'POST']}
              fallback="Default · GET"
            />
          )}
          <BooleanField
            path={['http', 'catalog']}
            label="Normalized catalog"
            help="Enable only for a compatible catalog endpoint."
          />
          {torznab && <MapField path={['http', 'query']} label="Base query · Full and all modes" />}
          {torznab && (
            <MapField path={['http', 'incremental_query']} label="Incremental query overrides" />
          )}
          {torznab && <JSONField path={['http', 'body']} label="Request body · JSON" multiline />}
        </AdvancedSection>
      )}
    </>
  )
}

function PaginationSettings() {
  const { document } = useSourceEditor()
  if (document.get('adapter') === 'html') return <HTMLPaginationSettings />
  const torznab = document.get('adapter') === 'torznab'
  const strategy = document.getIn(['pagination', 'type']) || 'none'
  const parameters = [
    {
      key: 'page_param',
      label: 'Page parameter',
      placeholder: 'page',
      active: strategy === 'page',
    },
    {
      key: 'offset_param',
      label: 'Offset parameter',
      placeholder: 'offset',
      active: strategy === 'offset',
    },
    {
      key: 'cursor_param',
      label: 'Cursor parameter',
      placeholder: 'cursor',
      active: strategy === 'cursor',
    },
    {
      key: 'size_param',
      label: 'Page-size parameter',
      placeholder: 'limit',
      active: strategy !== 'none',
    },
  ]
  const fields = (
    <>
      <FieldSection title="Page strategy">
        <FieldGrid>
          <SelectField
            path={['pagination', 'type']}
            label="Pagination type"
            choices={['none', 'page', 'offset', 'cursor']}
            fallback="Default · none"
          />
          {(strategy === 'page' || strategy === 'offset') && (
            <JSONField
              path={['pagination', 'start']}
              label="Starting page or offset"
              integer
              placeholder={strategy === 'offset' ? '0' : '1'}
              help={
                strategy === 'offset'
                  ? 'Most APIs start offsets at 0.'
                  : 'Most APIs start pages at 1. Check your source’s convention.'
              }
            />
          )}
        </FieldGrid>
      </FieldSection>
      <FieldSection title="Request parameter names">
        <FieldGrid>
          {parameters
            .filter((field) => field.active)
            .map((field) => (
              <TextField
                key={field.key}
                path={['pagination', field.key]}
                label={field.label}
                placeholder={field.placeholder}
              />
            ))}
        </FieldGrid>
        <AdvancedSection
          title="Other pagination parameters"
          description="Changing strategy keeps these settings; it never removes fields from the definition."
        >
          <FieldGrid>
            <SelectField
              path={['pagination', 'in']}
              label="Parameter location"
              choices={['query', 'body']}
              fallback="Default · query"
              help="Body pagination requires POST."
            />
            {strategy !== 'page' && strategy !== 'offset' && (
              <JSONField path={['pagination', 'start']} label="Starting page or offset" integer />
            )}
            {parameters
              .filter((field) => !field.active)
              .map((field) => (
                <TextField
                  key={field.key}
                  path={['pagination', field.key]}
                  label={field.label}
                  placeholder={field.placeholder}
                />
              ))}
          </FieldGrid>
        </AdvancedSection>
      </FieldSection>
      <FieldSection title="Response pointers">
        <FieldGrid>
          <TextField
            path={['pagination', 'next_path']}
            label="Next page / cursor pointer"
            placeholder="/meta/next"
            help="Use when the response supplies the next URL or cursor. Empty stops at an empty page."
          />
        </FieldGrid>
        <AdvancedSection
          title="Response checks"
          configuredPaths={[
            ['pagination', 'current_path'],
            ['pagination', 'total_path'],
          ]}
        >
          <FieldGrid>
            <TextField
              path={['pagination', 'total_path']}
              label="Total pointer"
              placeholder="/meta/total"
            />
            <TextField
              path={['pagination', 'current_path']}
              label="Current page pointer"
              placeholder="/meta/current_page"
            />
          </FieldGrid>
        </AdvancedSection>
      </FieldSection>
    </>
  )
  return (
    <>
      <FieldSection title="Page size">
        <JSONField
          path={['page_size']}
          label="Page size"
          integer
          placeholder="100"
          help="Records requested per page. Use the maximum allowed by your source."
        />
      </FieldSection>
      {torznab ? (
        <>
          <p className="notice">Torznab reads pagination from its RSS response.</p>
          <AdvancedSection title="Pagination settings">{fields}</AdvancedSection>
        </>
      ) : (
        fields
      )}
    </>
  )
}

function MappingSettings() {
  const { document } = useSourceEditor()
  const torznab = document.get('adapter') === 'torznab'
  const html = document.get('adapter') === 'html'
  const Selection = torznab ? AdvancedSection : FieldSection
  return (
    <>
      {html ? (
        <HTMLMappingSettings />
      ) : (
        <>
          {torznab && (
            <AdvancedSection
              title="Native identity"
              configuredPaths={[
                ['options', 'id_source'],
                ['options', 'id_pattern'],
              ]}
            >
              <FieldGrid>
                <SelectField
                  path={['options', 'id_source']}
                  label="Native ID from"
                  choices={['guid', 'link']}
                  fallback="RSS identifier (default)"
                  labels={{ guid: 'RSS identifier', link: 'Item link' }}
                  help="Choose the field containing the source’s stable item identifier."
                />
                <TextField
                  path={['options', 'id_pattern']}
                  label="Native ID pattern"
                  placeholder="/items/([0-9]+)"
                  help="Optional regular expression with one capture group for the native ID. Requires an explicit ID field."
                />
              </FieldGrid>
            </AdvancedSection>
          )}
          {torznab && (
            <p className="notice">
              Torznab reads torrent fields from its native RSS format. Custom JSON field settings
              remain accessible below. These fields do not configure IMDb identification.
            </p>
          )}
          <Selection
            title="Record selection"
            description="JSON mappings use JSON Pointers (RFC 6901), relative to the response or each item as noted. Torznab uses its native RSS mapping."
          >
            <FieldGrid>
              <TextField
                path={['http', 'items_path']}
                label="Items pointer"
                placeholder="/data"
                help="Array of records in the response. Empty selects the response root."
              />
              <TextField
                path={['mapping', 'id']}
                label="Native source ID pointer"
                placeholder="/id"
                help="Stable identity within this source, independent from the torrent hash."
              />
            </FieldGrid>
            <MapField
              path={['mapping', 'fields']}
              label="Field mappings"
              strings
              suggestions={[
                'title',
                'info_hash',
                'size',
                'published_at',
                'imdb_id',
                'tmdb_id',
                'tvdb_id',
                'season',
                'episode',
                'audiolangs',
                'categories',
              ]}
              help="Map catalog field names to pointers relative to each item, such as title → /name."
            />
          </Selection>
        </>
      )}
      <AdvancedSection
        title="Output selection"
        configuredPaths={[['output', 'fields']]}
        description="Output fields control presentation. Storage → Settings → Data retention controls which mapped fields and processing details are retained. Leave empty for the default output."
      >
        <StringArrayField
          path={['output', 'fields']}
          label="Output fields"
          help="Use field names or attributes.NAME selectors."
        />
      </AdvancedSection>
    </>
  )
}

function TraversalSettings() {
  const { document, change } = useSourceEditor()
  const configured =
    document.kindIn(['traversal']) !== undefined && document.kindIn(['traversal']) !== 'null'
  const metadataSupported =
    isJSONAdapter(document.get('adapter')) &&
    !document.getIn(['http', 'catalog']) &&
    document.kindIn(['traversal', 'id_recovery']) === 'object' &&
    document.lengthIn(['traversal', 'enrich_fields']) > 0
  return (
    <AdvancedSection
      title="Bounded traversal"
      configuredPaths={[['traversal']]}
      description="Advanced JSON collection across overlapping search windows. Configure only when a source has a bounded listing and advertised coverage totals."
    >
      {configured ? (
        <>
          <Button
            variant="warning"
            size="sm"
            onClick={() => change((next) => next.delete('traversal'))}
          >
            Remove traversal configuration
          </Button>
          <FieldGrid>
            <JSONField path={['traversal', 'window_pages']} label="Window pages" integer />
            <JSONField
              path={['traversal', 'minimum_total']}
              label="Minimum expected total"
              integer
            />
            <SelectField
              path={['traversal', 'total_mode']}
              label="Coverage total mode"
              choices={['strict', 'at_least']}
              fallback="Default · strict"
            />
            <SelectField
              path={['traversal', 'incremental_order']}
              label="Incremental listing order"
              choices={['id', 'published_at']}
              fallback="Default · id"
              help="ID order is checked strictly. Publication-sorted listings use known source IDs as the stopping boundary and tolerate date reordering."
            />
          </FieldGrid>
          <StringArrayField
            path={['traversal', 'total_paths']}
            label="Total pointers"
            help="Response pointers used to determine expected scope coverage."
          />
          <StringArrayField
            path={['traversal', 'enrich_fields']}
            label="Source detail fields"
            help="Fields filled by Metadata mode through ID recovery, not by discovery itself. An optional follow-up after Incremental can fill these fields for newly inserted torrents only."
          />
          <BooleanField
            path={['traversal', 'metadata_after_incremental']}
            label="Fetch source details after updates"
            allowEnabled={metadataSupported}
            help={
              metadataSupported
                ? 'Off by default. After a successful Incremental or its initial Full, queue a separate Metadata run only for newly inserted native torrents still missing configured fields. Existing torrents are excluded; no eligible new torrents means no follow-up.'
                : 'Unavailable: requires native HTTP/JSON, ID recovery and configured metadata enrichment fields. Remote catalogs are not supported. Disable this option or configure these prerequisites; it will not enrich this source as configured.'
            }
          />
          <ArrayField
            path={['traversal', 'scopes']}
            label="Scopes"
            initial={{ id: '', match: {} }}
            help="Each matching scope tracks its own observed native IDs."
          >
            {(path) => (
              <>
                <TextField path={[...path, 'id']} label="Scope identifier" />
                <MapField path={[...path, 'query']} label="Scope query" />
                <MapField
                  path={[...path, 'match']}
                  label="Mapped field matches"
                  help="Scalar values to match against mapped fields."
                />
              </>
            )}
          </ArrayField>
          <ArrayField
            path={['traversal', 'partitions']}
            label="Partitions"
            initial={{ parameter: '', start: 0, end: 0 }}
            help="Inclusive ranges. Specify either an end value or an end-year offset, never both."
          >
            {(path) => (
              <FieldGrid>
                <TextField path={[...path, 'parameter']} label="Partition parameter" />
                <JSONField path={[...path, 'start']} label="Partition start" integer />
                <JSONField
                  path={[...path, 'end']}
                  label="Partition end"
                  integer
                  help="Clear when using an end-year offset."
                />
                <JSONField
                  path={[...path, 'end_year_offset']}
                  label="End-year offset"
                  integer
                  help="Clear when using an explicit end."
                />
              </FieldGrid>
            )}
          </ArrayField>
          <ArrayField path={['traversal', 'query_variants']} label="Query variants" initial={{}}>
            {(path) => <MapField path={path} label="Variant parameters" />}
          </ArrayField>
          <TraversalOptions />
          <RecoverySettings />
        </>
      ) : (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            change((next) =>
              next.set('traversal', { window_pages: 10, total_paths: [], scopes: [] }),
            )
          }
        >
          Configure bounded traversal
        </Button>
      )}
    </AdvancedSection>
  )
}

function TraversalOptions() {
  const { document, change } = useSourceEditor()
  const configured =
    document.kindIn(['traversal', 'options']) !== undefined &&
    document.kindIn(['traversal', 'options']) !== 'null'
  return (
    <AdvancedSection
      title="Discovered filter options"
      description="Read available filters from an endpoint and use them to refine bounded queries. URL placeholders may reference scope query names."
    >
      {configured ? (
        <>
          <Button
            variant="warning"
            size="sm"
            onClick={() => change((next) => next.deleteIn(['traversal', 'options']))}
          >
            Remove discovered options
          </Button>
          <TextField path={['traversal', 'options', 'url']} label="Options URL" />
          <FieldGrid>
            <TextField path={['traversal', 'options', 'groups_path']} label="Groups pointer" />
            <TextField path={['traversal', 'options', 'values_path']} label="Values pointer" />
            <TextField path={['traversal', 'options', 'value_path']} label="Value pointer" />
            <TextField path={['traversal', 'options', 'query_param']} label="Query parameter" />
            <TextField path={['traversal', 'options', 'priority_path']} label="Priority pointer" />
          </FieldGrid>
        </>
      ) : (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            change((next) =>
              next.setIn(['traversal', 'options'], {
                url: '',
                groups_path: '',
                values_path: '',
                value_path: '',
                query_param: '',
              }),
            )
          }
        >
          Configure discovered options
        </Button>
      )}
    </AdvancedSection>
  )
}

function RecoverySettings() {
  const { document, change } = useSourceEditor()
  const configured =
    document.kindIn(['traversal', 'id_recovery']) !== undefined &&
    document.kindIn(['traversal', 'id_recovery']) !== 'null'
  const base = ['traversal', 'id_recovery']
  return (
    <AdvancedSection
      title="ID recovery & detail mapping"
      description="Recover finite native-ID ranges and resolve same-origin detail URLs. These settings configure the existing recovery engine; no custom code is executed."
    >
      {configured ? (
        <>
          <Button variant="warning" size="sm" onClick={() => change((next) => next.deleteIn(base))}>
            Remove ID recovery
          </Button>
          <MapField path={[...base, 'discovery_query']} label="Discovery query" />
          <FieldGrid>
            <JSONField path={[...base, 'first']} label="First native ID" integer />
            <TextField
              path={[...base, 'resolve_url']}
              label="Resolve URL"
              help="Use {id} for the requested numeric ID."
            />
            <TextField path={[...base, 'resolve_pattern']} label="Resolve path pattern" />
            <TextField
              path={[...base, 'detail_url']}
              label="Detail URL"
              help="Use {value} for the resolved last path segment."
            />
            <TextField path={[...base, 'detail_path']} label="Detail record pointer" />
            <TextField path={[...base, 'mapping', 'id']} label="Detail native ID pointer" />
          </FieldGrid>
          <MapField path={[...base, 'mapping', 'fields']} label="Detail field mappings" strings />
        </>
      ) : (
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            change((next) =>
              next.setIn(base, {
                discovery_query: {},
                first: 1,
                resolve_url: '',
                resolve_pattern: '',
                detail_url: '',
                detail_path: '',
                mapping: { id: '', fields: {} },
              }),
            )
          }
        >
          Configure ID recovery
        </Button>
      )}
    </AdvancedSection>
  )
}

function RequestLimitsSettings() {
  const { document, change } = useSourceEditor()
  return (
    <AdvancedSection
      title="Rolling request budgets"
      configuredPaths={[['request_limits']]}
      description="Optional limits shared by all runs of this source, including retries. Each rolling window counts recent requests, not calendar resets. Request spacing still applies."
    >
      <FieldGrid>
        <JSONField
          path={['request_limits', 'per_minute']}
          label="Requests per minute"
          integer
          help="Previous 60 seconds. 0 or empty disables this window."
        />
        <JSONField
          path={['request_limits', 'per_hour']}
          label="Requests per hour"
          integer
          help="Previous 3,600 seconds. 0 or empty disables this window."
        />
        <JSONField
          path={['request_limits', 'per_day']}
          label="Requests per day"
          integer
          help="Previous 86,400 seconds. 0 or empty disables this window."
        />
      </FieldGrid>
      <p className="help">
        Use whole numbers up to 1,000,000. Keep at least one positive limit, or remove all budgets.
      </p>
      {document.kindIn(['request_limits']) !== undefined && (
        <Button
          variant="warning"
          size="sm"
          onClick={() => change((next) => next.delete('request_limits'))}
        >
          Remove all request budgets
        </Button>
      )}
    </AdvancedSection>
  )
}

function SourceDurationField({
  path,
  label,
  choices,
}: {
  path: string[]
  label: string
  choices: ReadonlyArray<readonly [string, string]>
}) {
  const { document, change } = useSourceEditor()
  const value = document.getIn(path)
  return (
    <DurationSelect
      label={label}
      value={typeof value === 'string' ? value : ''}
      choices={choices}
      emptyLabel="Use default"
      onChange={(duration) =>
        change((next) => (duration ? next.setIn(path, duration) : next.deleteIn(path)))
      }
    />
  )
}

function CollectionSettings() {
  const { document } = useSourceEditor()
  const torznab = document.get('adapter') === 'torznab'
  return (
    <>
      <FieldSection
        title="Request pacing"
        description="Source-level settings apply to future runs. Existing run snapshots keep their original configuration."
      >
        <FieldGrid>
          <SourceDurationField
            path={['request_interval']}
            label="Request interval"
            choices={[
              ['500ms', 'Half a second'],
              ['1s', '1 second'],
              ['2s', '2 seconds'],
              ['5s', '5 seconds'],
              ['10s', '10 seconds'],
            ]}
          />
          <SourceDurationField
            path={['request_timeout']}
            label="Request timeout"
            choices={[
              ['10s', '10 seconds'],
              ['30s', '30 seconds'],
              ['60s', '1 minute'],
              ['120s', '2 minutes'],
            ]}
          />
        </FieldGrid>
        <AdvancedSection title="Rate-limit headers" configuredPaths={[['rate_limit_reset']]}>
          <SelectField
            path={['rate_limit_reset']}
            label="Rate-limit reset interpretation"
            choices={['epoch', 'relative']}
            fallback="Default · epoch"
          />
        </AdvancedSection>
      </FieldSection>
      <RequestLimitsSettings />
      <AdvancedSection
        title="Adapter options"
        configuredPaths={[['options']]}
        description="Adapter-specific options are preserved as arbitrary JSON. Configure only options supported by the selected adapter."
      >
        <MapField
          path={['options']}
          label="Options"
          excludeKeys={torznab ? ['search_mode', 'category_scope', 'id_source', 'id_pattern'] : []}
        />
      </AdvancedSection>
      {document.get('adapter') !== 'html' && <TraversalSettings />}
    </>
  )
}

function SourceScheduleSettings() {
  const { document, change, reportDraft } = useSourceEditor()
  const id = useId()
  const text = (key: string) => {
    const value = document.getIn(['schedule', key])
    return typeof value === 'string' ? value : ''
  }
  const every = text('every'),
    cron = text('cron'),
    fullEvery = text('full_every')
  const [selectedTiming, setTiming] = useState<TimingMode | null>(null)
  const timing = selectedTiming ?? timingMode(every, cron)
  const configured = Boolean(every || cron)
  const storedEnabled = document.getIn(['schedule', 'enabled'])
  const enabled = typeof storedEnabled === 'boolean' ? storedEnabled : configured
  const mode = text('mode') || 'incremental'
  const metadataSupported =
    isJSONAdapter(document.get('adapter')) &&
    document.getIn(['http', 'catalog']) !== true &&
    document.getIn(['traversal', 'id_recovery']) != null &&
    document.lengthIn(['traversal', 'enrich_fields']) > 0
  const invalidPolicy =
    (mode === 'metadata' && !metadataSupported) ||
    Boolean(fullEvery && (mode !== 'incremental' || timing === 'manual'))
  const reportTiming = useCallback(
    (invalid: boolean) => reportDraft(id + '-timing', invalid),
    [id, reportDraft],
  )
  useEffect(() => {
    reportDraft(id + '-policy', invalidPolicy)
    return () => reportDraft(id + '-policy', false)
  }, [id, invalidPolicy, reportDraft])
  return (
    <>
      <FieldSection
        title="Automatic imports"
        description="These settings are saved with this source. Both the source and its schedule must be enabled. The server runs imports even when this page is closed."
      >
        <div className="flex items-center justify-between gap-4 rounded-md border p-3">
          <div>
            <Label htmlFor={id}>Schedule enabled</Label>
            <p className="help mt-1">
              Disabling keeps the frequency and does not stop an existing run.
            </p>
          </div>
          <Switch
            id={id}
            checked={enabled}
            onCheckedChange={(value) =>
              change((next) => next.setIn(['schedule', 'enabled'], value))
            }
          />
        </div>
        <ScheduleTimingFields
          timing={timing}
          every={every}
          cron={cron}
          timezone={text('timezone') || 'UTC'}
          onValidityChange={reportTiming}
          onChange={(patch) => {
            if (patch.timing !== undefined) setTiming(patch.timing)
            change((next) => {
              for (const key of ['every', 'cron', 'timezone'] as const) {
                const value = patch[key]
                if (value !== undefined)
                  value ? next.setIn(['schedule', key], value) : next.deleteIn(['schedule', key])
              }
            })
          }}
        />
        <SelectField
          path={['schedule', 'mode']}
          label="Collection mode"
          fallback="Update (default)"
          choices={
            metadataSupported ? ['incremental', 'full', 'metadata'] : ['incremental', 'full']
          }
          labels={{
            incremental: 'Update',
            full: 'Rebuild source catalog',
            metadata: 'Fetch source details',
          }}
          help="Updates start with a complete import until a successful baseline exists. Rebuilds publish only after the complete collection succeeds. Source details enrich existing torrents without discovering or removing them."
        />
        {invalidPolicy && (
          <p className="help text-destructive" role="alert">
            Source details require configured metadata enrichment. A periodic rebuild requires an
            automatic Update schedule. Review the mode and advanced settings before saving.
          </p>
        )}
      </FieldSection>
      <AdvancedSection title="Advanced schedule settings">
        <DurationSelect
          label="Rebuild whole source periodically"
          value={fullEvery}
          emptyLabel="Disabled"
          onChange={(value) =>
            change((next) =>
              value
                ? next.setIn(['schedule', 'full_every'], value)
                : next.deleteIn(['schedule', 'full_every']),
            )
          }
        />
        <FieldGrid>
          <JSONField
            path={['schedule', 'max_pages']}
            label="Page budget per tick"
            integer
            help="0 = unlimited. A budget-limited scheduled run continues on the next tick."
          />
          <JSONField
            path={['schedule', 'known_pages']}
            label="Known-page boundary"
            integer
            help="Update only: stop after this many consecutive entirely known pages per scope. 0 = off."
          />
        </FieldGrid>
      </AdvancedSection>
      <p className="help">
        Existing runs keep their original settings and committed checkpoint. Editing a paused
        scheduled source can require resuming or cancelling its old run in History. Missed ticks do
        not create catch-up bursts.
      </p>
    </>
  )
}

export function SourceEditor({
  value,
  onChange,
  readOnly,
  sourceID,
  onBlockedChange,
  initialStage,
  mode: requestedMode,
  onModeChange,
  jsonActions,
}: {
  value: string
  onChange: (value: string) => void
  readOnly: boolean
  sourceID?: string
  onBlockedChange: (blocked: boolean) => void
  initialStage?: string
  mode: 'visual' | 'json'
  onModeChange: (mode: 'visual' | 'json') => void
  jsonActions?: ReactNode
}) {
  const document = useMemo(() => parseProviderJSON(value), [value])
  const sourceStages = document.getIn(['http', 'catalog']) === true ? remoteStages : stages
  const mode = document.errors.length ? 'json' : requestedMode
  const [selectedIndex, setSelected] = useState(() =>
    Math.max(
      0,
      stages.findIndex((stage) => stage.id === initialStage),
    ),
  )
  const selected = Math.min(selectedIndex, sourceStages.length - 1)
  useEffect(() => {
    const index = sourceStages.findIndex((stage) => stage.id === initialStage)
    setSelected((previous) => (index >= 0 ? index : Math.min(previous, sourceStages.length - 1)))
  }, [initialStage, sourceStages])
  const [editError, setEditError] = useState('')
  const [drafts, setDrafts] = useState<Set<string>>(() => new Set())
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  const id = useId()
  const reportDraft = useCallback((key: string, invalid: boolean) => {
    setDrafts((previous) => {
      if (previous.has(key) === invalid) return previous
      const next = new Set(previous)
      if (invalid) next.add(key)
      else next.delete(key)
      return next
    })
  }, [])
  useEffect(() => {
    onBlockedChange(drafts.size > 0 || Boolean(editError))
  }, [drafts, editError, onBlockedChange])
  const change = useCallback(
    (edit: (document: ProviderJSON) => void) => {
      if (readOnly) return
      try {
        const next = parseProviderJSON(value)
        edit(next)
        onChange(next.toString())
        setEditError('')
      } catch {
        setEditError(
          'This field cannot be changed safely. Repair its containing value in the JSON definition.',
        )
      }
    },
    [value, onChange, readOnly],
  )
  const text = (path: string[], fallback: string) => {
    const entry = document.getIn(path)
    return typeof entry === 'string' && entry ? entry : fallback
  }
  let endpoint = 'No endpoint configured'
  try {
    endpoint = new URL(text(['url'], '')).hostname
  } catch {
    /* Invalid endpoints remain editable. */
  }
  const summaries = [
    `${text(['adapter'], 'Choose adapter')} · ${document.get('enabled') === true ? 'enabled' : 'disabled'}`,
    `${text(['http', 'method'], 'GET')} · ${endpoint}`,
    document.get('adapter') === 'html' && !document.getIn(['pagination', 'size_param'])
      ? text(['pagination', 'type'], 'next')
      : `${text(['pagination', 'type'], document.get('adapter') === 'html' ? 'next' : 'none')} · ${document.rawIn(['page_size']) || 'default'} records`,
    document.get('adapter') === 'html'
      ? `${document.keysIn(['html', 'fields']).length} listing fields · CSS selectors`
      : `${document.keysIn(['mapping', 'fields']).length} fields · ${text(['http', 'items_path'], 'root items')}`,
    `${text(['request_interval'], '1s')} between requests`,
    text(['schedule', 'every'], text(['schedule', 'cron'], 'Manual only')),
  ]
  const current = sourceStages[selected]
  return (
    <SourceEditorContext.Provider value={{ document, change, reportDraft, readOnly }}>
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div
            className="inline-flex gap-1 rounded-md border border-border bg-card p-1"
            role="group"
            aria-label="Definition editor mode"
          >
            <Button
              size="sm"
              variant={mode === 'visual' ? 'secondary' : 'ghost'}
              aria-pressed={mode === 'visual'}
              disabled={document.errors.length > 0 || drafts.size > 0}
              onClick={() => onModeChange('visual')}
            >
              <FlowArrowIcon />
              Visual stages
            </Button>
            <Button
              size="sm"
              variant={mode === 'json' ? 'secondary' : 'ghost'}
              aria-pressed={mode === 'json'}
              disabled={drafts.size > 0}
              onClick={() => onModeChange('json')}
            >
              <BracketsCurlyIcon />
              JSON
            </Button>
          </div>
          {mode === 'json' && jsonActions && (
            <div className="flex flex-wrap gap-2">{jsonActions}</div>
          )}
        </div>
        {drafts.size > 0 && (
          <p className="notice notice-warning" role="status">
            Finish or discard the pending field draft before switching stages, validating, importing
            or saving.
          </p>
        )}
        {editError && (
          <p className="notice notice-error" role="alert">
            {editError}
          </p>
        )}
        {document.errors.length > 0 && (sourceID || value.trim() !== '') && (
          <div className="notice notice-error" role="alert">
            <p className="font-medium">Repair the JSON to enable visual editing and saving.</p>
            <ul className="mt-2 list-inside list-disc">
              {document.errors.map((error, index) => (
                <li key={index}>{error}</li>
              ))}
            </ul>
          </div>
        )}
        {mode === 'json' || document.errors.length > 0 ? (
          <>
            <CodeEditor
              value={value}
              onChange={(next) => {
                setEditError('')
                onChange(next)
              }}
              label="Source JSON definition"
              height="620px"
              readOnly={readOnly}
            />
            <p className="help">
              Strict JSON object · no comments, duplicate keys or trailing commas · 128 KiB maximum.
              Untouched numeric values retain their exact representation.
            </p>
          </>
        ) : (
          <div className="grid overflow-hidden rounded-md border border-border bg-card/30 lg:grid-cols-[255px_minmax(0,1fr)]">
            <aside className="border-b border-border bg-background/45 p-4 lg:border-r lg:border-b-0 lg:p-5">
              <div className="mb-5 flex items-center justify-between gap-2">
                <h2 className="text-xs font-medium uppercase tracking-widest text-muted-foreground">
                  Source stages
                </h2>
                <span className="mono text-primary">
                  {String(selected + 1).padStart(2, '0')} /{' '}
                  {String(sourceStages.length).padStart(2, '0')}
                </span>
              </div>
              <div
                role="tablist"
                aria-label="Source configuration stages"
                aria-orientation="vertical"
                className="space-y-0"
              >
                {sourceStages.map((stage, index) => {
                  const Icon = stage.icon
                  return (
                    <div key={stage.id}>
                      {index > 0 && (
                        <div
                          className="ml-[25px] h-3 w-px bg-primary/35 lg:h-6"
                          aria-hidden="true"
                        />
                      )}
                      <button
                        type="button"
                        role="tab"
                        aria-label={stage.label}
                        id={`${id}-${stage.id}`}
                        aria-controls={`${id}-panel`}
                        aria-selected={selected === index}
                        tabIndex={selected === index ? 0 : -1}
                        ref={(element) => {
                          buttons.current[index] = element
                        }}
                        disabled={drafts.size > 0 && selected !== index}
                        className={`flex w-full items-center gap-3 rounded-md border px-3 py-3 text-left transition-colors disabled:opacity-50 ${selected === index ? 'border-primary/60 bg-primary/8' : 'border-border bg-card/70 hover:border-primary/30 hover:bg-accent'}`}
                        onClick={() => setSelected(index)}
                        onKeyDown={(event) => {
                          if (drafts.size > 0) return
                          const next =
                            event.key === 'ArrowDown' || event.key === 'ArrowRight'
                              ? (index + 1) % sourceStages.length
                              : event.key === 'ArrowUp' || event.key === 'ArrowLeft'
                                ? (index + sourceStages.length - 1) % sourceStages.length
                                : event.key === 'Home'
                                  ? 0
                                  : event.key === 'End'
                                    ? sourceStages.length - 1
                                    : -1
                          if (next >= 0) {
                            event.preventDefault()
                            setSelected(next)
                            buttons.current[next]?.focus({ preventScroll: true })
                          }
                        }}
                      >
                        <span
                          className={`flex size-7 shrink-0 items-center justify-center rounded border ${selected === index ? 'border-primary/40 text-primary' : 'border-border text-muted-foreground'}`}
                        >
                          <Icon size={17} />
                        </span>
                        <span className="min-w-0">
                          <span className="block text-sm font-medium">{stage.label}</span>
                          <span
                            className={`mt-1 break-words text-xs leading-5 text-muted-foreground ${selected === index ? 'block' : 'hidden lg:block'}`}
                          >
                            {summaries[index]}
                          </span>
                        </span>
                      </button>
                    </div>
                  )
                })}
              </div>
              <p className="help mt-6 hidden lg:block">
                Select a stage to configure it. Connections show the fixed collection sequence, not
                custom execution logic.
              </p>
            </aside>
            <section
              role="tabpanel"
              id={`${id}-panel`}
              aria-labelledby={`${id}-${current.id}`}
              tabIndex={0}
              className="min-w-0 p-5 sm:p-6"
            >
              <header className="mb-7 flex items-start gap-4 border-b border-border pb-5">
                <span className="mono pt-1 text-primary">0{selected + 1}</span>
                <p className="help">{current.description}</p>
              </header>
              <fieldset disabled={readOnly} className="min-w-0 space-y-7 disabled:opacity-60">
                <legend className="sr-only">{current.label} settings</legend>
                {selected === 0 && <IdentitySettings sourceID={sourceID} />}
                {selected === 1 && <ConnectionSettings />}
                {selected === 2 && <PaginationSettings />}
                {selected === 3 && <MappingSettings />}
                {selected === 4 && <CollectionSettings />}
                {selected === 5 && <SourceScheduleSettings />}
              </fieldset>
            </section>
          </div>
        )}
      </div>
    </SourceEditorContext.Provider>
  )
}
