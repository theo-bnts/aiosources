# Public release verification

Verified on 17 October 2026 using the embedded production application, PostgreSQL 18.6 and disposable databases. Source and service fixtures use fictitious addresses or isolated local servers; no production credential or private source definition is required.

## Completed local checks

| Check | Result |
| --- | --- |
| Complete application Go suite with race detection | Passed with a fresh run against isolated PostgreSQL |
| Standalone Torznab SDK with race detection | Passed with a fresh run |
| Go static analysis and formatting, both modules | Passed |
| Frontend unit tests | 32 passed |
| Frontend formatting, type checking and production build | Passed |
| Production browser suite with real isolated PostgreSQL | 125 passed, including desktop and mobile |
| Production Linux amd64 and arm64 binaries | Compiled successfully |
| Deployment and README Compose examples | Validated |
| Redacted secret scan of intended publication files | No findings |
| Source-aware Go vulnerability scans | No affected imported package or reachable call; see the module-only advisory in [Security](../SECURITY.md) |
| Frontend dependency audit | No reported vulnerabilities |
| Existing GitHub workflows and original MIT license | Preserved unchanged |
| Image channel metadata and promotion guards | Six Python test cases passed; preview/draft releases, altered digests, mismatched commits/registries and unsafe tags are rejected |
| GitHub preview and stable workflows | Owner-installed workflows retained unchanged; preview publication and manual stable-promotion guards preserved |
| Actual upstream AIOStreams parser | Ten synthetic direct/debrid/season-pack responses accepted with their supported metadata preserved |

Browser and frontend counts are test cases. Go packages with no tests are reported separately by the Go runner.

## Behavior covered

- Source previews, initial Full, Incremental and explicit rebuilds; immutable snapshots; page, scope and publication integrity; pause/cancel/resume; rate limits, technical interruptions and restart recovery.
- Film/series identification, trusted native IDs, uncertain and manual matches, episode and season-pack handling, generation invalidation and indexed candidate lookup.
- AllDebrid/TorBox availability contracts, magnet formatting and file selection; account isolation; temporary-entry ownership and durable cleanup.
- Private Stremio installation tokens, returned results and request histories; catalog share scope, revocation, pagination and remote checkpoints.
- Independent named Stremio installations use saved source, media, service, cache and language filters. Updating settings preserves the installation URL and manifest identity, including when a client retains its manifest. Disabled, expired, rotated and revoked keys fail closed; current permissions are rechecked before delivering a response. Only named private installation links are accepted; the retired global token and its URL options are rejected.
- Catalog, Torznab and Stremio access credentials are protocol-specific. Named Stremio keys are hashed, optional request and concurrency quotas are durable, and changing permissions or revoking an access blocks an in-flight response. Playback retains its existing preparation deadline. Upgrade coverage preserves prior public consent and Torznab keys without enabling catalog-history retention. New private accesses default to all sources without per-access limits; Advanced policies remain opt-in. Migration removes the obsolete global key while preserving named credentials and explicit policies.
- Public Stremio and Torznab keep optional controls in Filters, followed by Advanced when permitted. Submitting clears credentials while preserving filters and creates no saved visitor grant or admission record. Personal announce URLs and debrid keys never enter browser storage or database access settings. Public and administrator credentials cannot substitute for one another.
- Cached-only private Stremio and Torznab exclude uncached, unknown and expired availability; playback cannot fall back to an administrator announce. Cached-only public Torznab also omits visitor announces from its returned magnets. Coverage includes unavailable playback and episode-specific cache evidence.
- Seven-day diagnostic retention without deleting published data or progress required by unfinished operations.
- Authenticated API access, CSRF, sign-in throttling, cookie restrictions, two-step verification, session revocation and private filesystem/vault handling. A direct-HTTP regression verifies that spoofed forwarded headers cannot change the trusted origin or evade connection-based throttling.
- Environment-name migration fails before generating private state. The default concurrency is five, while saved settings survive restart. The retained workflow's old test-only database name still runs actual integration tests.
- Source drafts, concurrent edits, exact JSON numbers, navigation, forms, histories, paging and mobile layouts. Closing navigation or credits leaves source drafts intact; normal route changes and sign-out retain their unsaved-change guards.
- Source Run/Delete actions retain their horizontal alignment when the unsaved-change hint appears on desktop and mobile. Removing a debrid key keeps an existing service filter editable without silently widening its saved policy.
- Standard stream descriptions, structured indexer/service/cache/date/duration hints, debrid hashes, selected-file versus folder sizes, and web readiness. Unknown cache values and invalid optional metadata are omitted; a pack's aggregate duration is not attached to its selected episode. The real [AIOStreams parser](https://github.com/Viren070/AIOStreams/blob/b3170f148fff5703bf06f36e0db6dc799e6a76cf/packages/core/src/parser/streams.ts) was exercised with its file parser and dependencies; only logging and unrelated runtime configuration were isolated.
- An anonymous-access matrix covers private administration routes and verifies that installation/share credentials cannot substitute for administrator authentication. Public login/static assets and minimal health/session responses remain intentional.

## Public files and attribution

Reviewed code, documentation, fixtures and bundled assets contain no configured content source, private source-site URL, default announce address or account credential. Historical private commit references and obsolete screenshots are removed. This review does not establish the rights attached to user-supplied data.

The original MIT copyright is retained, including in the standalone SDK. Bundled notices reproduce original licenses for 13 production Go modules, 119 frontend libraries/fonts/emitted helpers the Go standard library and the image’s Mozilla/Alpine certificate bundle. Its exact source archive is checksum-verified and linked alongside MPL-2.0. Reviewed upstream snapshots cover npm packages whose archives omit their licenses. The approved, unchanged TMDB logo and required attribution are available in About AIOSources; the browser regression verifies local assets, distributed notice text and draft preservation.

## Publication and practical limits

The retained GitHub workflow gates image publication on lint, race and browser checks, then builds Linux amd64 and arm64. Verify its result and the published image's commit tag/digest for each release. A local Docker engine was unavailable for this review; local binary compilation is not a substitute for that container build.

No live TMDB, AllDebrid, TorBox or private-source account was used. Local contract tests do not establish provider availability or permissions for every real account. No review or scan guarantees legal compliance for every deployment or the absence of every security vulnerability.

The application serves HTTP. Direct access belongs on a trusted private network or inside an encrypted tunnel; public internet access requires HTTPS termination. PUBLIC_URL describes the public origin and does not provide TLS. Git history rewriting also cannot erase other copies or guarantee deletion of GitHub's internal objects; see [Legal](../LEGAL.md).
