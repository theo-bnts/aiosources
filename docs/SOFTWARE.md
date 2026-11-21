# Software

[Back to README](../README.md) · [Search and identification](OPERATIONS.md) · [Torznab](TORZNAB.md)

AIOSources can identify software applications already present in your imported sources using a 100% local bulk catalog combining **NIST NVD Official CPE Dictionary 2.0**, **Microsoft WinGet**, **Homebrew Cask**, and optional remote icon metadata from **selfh.st/icons**, then offer them through Search and Torznab. Software has its own **Statistics**, **Settings** and **History** under **Metadata → Catalogs → Software**. Search, matching and review run entirely against your local PostgreSQL catalog; your torrent titles and search queries are never sent to external APIs.

## Data sources and licensing

All four sources are permissive or public domain (no *ShareAlike* sources):

| Source | Role | License | Feed |
| --- | --- | --- | --- |
| **NIST NVD Official CPE Dictionary 2.0** | Broad commercial and cross-platform software catalog (`part="a"`, optionally `part="o"` for OS ISOs) | US Gov Public Domain | `nvdcpe-2.0.tar.gz` + `.meta` SHA-256 verification + global incremental API `services.nvd.nist.gov/rest/json/cpes/2.0` |
| **Microsoft WinGet (`microsoft/winget-pkgs`)** | Windows desktop applications, descriptions, versions, publishers, tags, monikers and licenses | MIT | Streaming archive from `codeload.github.com/microsoft/winget-pkgs/tar.gz/refs/heads/master` |
| **Homebrew Cask** | macOS desktop applications, aliases, descriptions, homepages and versions | BSD-2-Clause | Bulk JSON from `formulae.brew.sh/api/cask.json` |
| **selfh.st/icons** *(optional, ON by default)* | Remote color PNG icon availability metadata only (~3,050 apps; zero image bytes stored on disk) | CC BY 4.0 | `raw.githubusercontent.com/selfhst/icons/main/index.json` |

Entries are deterministically deduplicated across sources by normalized official homepage domain (excluding generic hosts such as `github.com` or `sourceforge.net`, where exact `owner/repo` matching is used), then by normalized `(vendor, product)`, then by canonical name and vendor. Metadata descriptions and categories prefer **WinGet > Homebrew Cask > NVD CPE**, and platforms are merged across sources (`windows`, `macos`, `linux`, `android`, `ios`).

## Set up

1. Open **Metadata → Catalogs → Software → Settings**.
2. Optionally configure an **NVD API key (optional)**:
   - Without a key, NVD CPE sync works automatically at the public rate limit (**5 requests per 30 seconds**).
   - Request a free personal key at [nvd.nist.gov/developers/request-an-api-key](https://nvd.nist.gov/developers/request-an-api-key) and save it via **Add NVD API key** (encrypted in **Secrets**) to accelerate incremental NVD updates up to **50 requests per 30 seconds**.
   - The key is never returned in API responses or written to logs.
3. Optionally adjust **Catalog filters**:
   - **Include commercial & vendor catalog (NVD CPE)** (enabled by default): includes NIST NVD CPE 2.0 entries alongside WinGet and Homebrew Cask.
   - **Exclude web plugins, CMS extensions and developer libraries** (enabled by default): filters out WordPress, Drupal, Joomla and Jenkins plugins, npm/PyPI/RubyGems/Maven developer packages, browser extensions and hardware firmwares from NVD CPE so the catalog stays focused on real applications.
   - **Include operating systems** (disabled by default): enables NVD `cpe:2.3:o:` entries to identify Windows, Linux and macOS installation ISOs.
   - **Include software icons index** (enabled by default): imports lightweight metadata from `selfh.st/icons` (`PNG == "Yes"`). Only validated lowercase slugs are retained and rendered directly from `https://cdn.jsdelivr.net/gh/selfhst/icons@main/png/<ref>.png` in the user's browser — arbitrary publisher icon URLs are never stored or served.
   - **Platforms**: choose which operating systems to include (`Windows`, `macOS`, `Linux`, `Android`, `iOS` — all five enabled by default).
4. Enable **Automatically update software information** and save, or run **Manual actions → Update software information**.

Changing catalog filters triggers an automatic re-projection or refresh; entries outside the new selection are removed only after the rebuilt catalog finishes, keeping the existing catalog searchable throughout.

## Long pauses and automatic recovery

NVD's incremental CPE API accepts `lastModStartDate` / `lastModEndDate` windows of up to 120 consecutive days. If software updates are paused or disabled for more than **110 days** (for example 3 months), or if incremental continuity fails or returns an excessive volume of changes, AIOSources automatically switches back to a **fresh full snapshot import** of `nvdcpe-2.0.tar.gz` (verified against `nvdcpe-2.0.meta`). Your existing searchable software catalog remains intact until the rebuild finishes and swaps cleanly.

## Find and identify software

Use **Search → Filters → Type → Software** to browse identified applications, inspect vendor, version, platforms and license details, follow deterministic links to **WinGet**, **Homebrew Cask** and **NVD CPE**, and download releases or add them to your debrid service.

Software is **not** exposed in Stremio feeds (same behavior as Games, Music and Books).

For Torznab clients such as **Prowlarr**, enable the **Software** media filter on a private or public Torznab connection. AIOSources maps Software to standard PC and Mobile categories:

- `4000` (PC), `4010` (0day), `4020` (ISO), `4030` (Mac), `4040` (Mobile-Other), `4060` (Mobile-iOS), `4070` (Mobile-Android)
- Category `4050` (**PC-Games**) is **strictly excluded** and remains reserved for [Games (IGDB)](GAMES.md).

Automatic identification parses software release names to extract the canonical application name, optional vendor, version (`v26.3.0`, `2025`, `CC 2024`, `Build 1234`), OS/platform (`Windows`, `Win64`, `x64`, `ARM64`, `macOS`, `Universal`, `Linux`, `AppImage`, `DEB`, `RPM`, `Android`, `APK`, `iOS`, `IPA`) and release tags (`Portable`, `Repack`, `Multilingual`, `Pro`, `Enterprise`, `Studio`, `VST`/`VST3`/`AU`/`AAX`). Anti-collision rules prevent PC game repacks (`4050`, FitGirl/DODI/RUNE/CODEX/SKIDROW/FLT/ElAmigos), ebooks/audiobooks (`7000`/`3030`) and music (`3000..3090`) from being misclassified as software, and prevent year-bearing software releases (such as `Adobe.Photoshop.2025.v26.0.Multilingual.x64`) from being hijacked by Videos or Games.

## Optional settings and maintenance

- **Automatic updates:** defaults to every **360 minutes** (configurable between 15 and 1440 minutes). Conditional requests (`ETag` / `nvdcpe-2.0.meta` SHA-256) skip unchanged bulk feeds.
- **Network → Outgoing → Routing → Software:** route NVD, WinGet, Homebrew Cask and `selfh.st/icons` downloads through a configured proxy if needed. Redirects are restricted to the official host allowlist (`nvd.nist.gov`, `services.nvd.nist.gov`, `codeload.github.com`, `raw.githubusercontent.com`, `formulae.brew.sh`).
- **Maintenance → Reset software information:** removes NVD CPE, WinGet, Homebrew Cask and `selfh.st/icons` catalog data, related automatic and manual software identifications, availability observations and software operation history. Videos, games, music, books, source records, settings and debrid files remain untouched.
