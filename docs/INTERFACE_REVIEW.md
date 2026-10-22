# Interface consistency review

[Back to README](../README.md) · [Application settings](CONFIGURATION_REVIEW.md)

The interface uses shared controls for public and administrator pages. Review covers navigation, page/workspace layouts, settings, cards, filters, tables, pagers, detail views, dialogs, confirmations, disclosures, loading states, notifications and form feedback. Existing browser regressions also exercise source editors, connection filters, identification review, secrets, two-step verification and downloads.

## Confirmed differences addressed

| Difference | Result |
| --- | --- |
| The date formatter forced English even when the display was described as following the browser. | One browser-local date and time-zone policy is used throughout the interface. |
| Most lists used shared pagination, but the per-source storage table hard-coded its own page size. | Both use the common 25/50/100-row preference. Each list keeps its own offset. |
| Refresh intervals had no common user control, and frequent progress events could override the slower statistics interval. | One Normal / Reduced activity preference applies throughout; progress events respect the statistics interval while explicit writes refresh immediately. |
| A long field label used a single-line line height even when it wrapped on mobile. | Shared labels use a readable line height for both single and multiple lines. |
| Expiration inputs always used local time, which would conflict with a UTC display preference. | Editable dates follow the selected display time zone and preserve untouched instants and fractional seconds. |
| History explanations hard-coded seven days. | They now refer to the saved retention policy; its actual duration is shown in Storage settings. |
| Storage mode was presented as Data retention, which would conflict with the new history controls. | Its panel is named Stored data; history has a separate folded section. |
| Operational settings could otherwise accumulate on everyday forms. | Infrequent controls use common folded sections, shared number fields, validation, save feedback and concurrent-edit protection. |
| Folded settings could create hidden loading skeletons before being opened. | Loading indicators are mounted only while the section is open; saved forms and drafts remain available when folded again. |
| Additional header controls could overlap narrow breadcrumbs. | The header reserves space for both Interface preferences and About at mobile and desktop widths. |
| Network settings placed Save on the opposite side from other settings forms. | Incoming and outgoing forms now use the same Save / status / conflict-action order. |

## Shared component checks

| Family | Components and pages covered |
| --- | --- |
| Navigation and layout | Public/admin sidebar groups, Network children, breadcrumbs, workspace tabs, return links, mobile navigation. |
| Forms | Inputs, password reveal, labels, native selects, multiple selection, switches, textareas, field help and validation; source, metadata, Debrid, connection, security, network and storage settings. |
| Lists | Search, Torrents, Identification, source/remote histories, operation details, Stremio history, Logs and storage breakdown; result counts, filter panels and pagination. |
| Secondary surfaces | Source block/JSON editor, import dialog, remote catalog editor, sharing/connection dialogs, secret creation, identification review and download preparation. |
| Feedback | FormStatus, Loading, Empty, ErrorNotice, notifications, warning/error notices, disabled actions and destructive confirmations. |
| Presentation | Panel/card spacing, statistic cards, table primitives, detail rows, disclosures, icons, action placement and responsive overflow. |

These checks do not treat every visual difference as a defect. Numeric code fields, large statistics, status colors, destructive actions and dense diagnostics intentionally have different presentations.

## Suggestions left for a separate design decision

- **Long source definitions:** an optional wider editor could help large mappings. The current block/JSON layout remains available; changing its workflow would deserve a dedicated review.
- **Dense history details:** a compact table density mode could help large screens, but should be a single shared option rather than custom spacing per page. No extra density selector was added.

No new general-purpose administration category was added for browser preferences. They are reachable in the same header location before and after administrator sign-in.
