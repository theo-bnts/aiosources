# Books and audiobooks

[Back to README](../README.md) · [Search and identification](OPERATIONS.md) · [Torznab](TORZNAB.md)

AIOSources can identify ebooks, comics and audiobooks already present in your imported sources, then offer them through Search and Torznab. It does not supply book sources or download books from Open Library.

## Set up book information

1. Import your authorized sources as usual. Keep useful fields such as the title, categories, author and ISBN when your source supplies them. Existing imported titles do not need to be downloaded again.
2. Open **Metadata → Books → Settings → Catalog filters**. **Relevant editions only** is on by default: it keeps editions with digital or audiobook evidence, including scans, and editions whose title or identifier matches books already imported when the catalog import starts. Turn it off to include print-only editions without this evidence.
3. Optionally enable **Filter by language** and select one or more edition languages. This filter starts **disabled**. Enabling it selects your browser language initially; the saved selection applies to the instance. Translations count. Unknown languages do not pass an active language filter.
4. In **Schedules**, enable **Automatically update book information**. No Open Library account or API key is required. Allow the first import to finish; it can take hours. Only selected editions and their related books and authors are stored. The exports still need to be read, so connection speed and server capacity affect the duration.
5. Open **Metadata → Identification → Settings** to enable automatic identification, or use **Advanced → Manual actions → Identify torrents** there after the book information import finishes.

Catalog filters are checked before saving bibliographic information and also apply to later updates. A book is included if at least one edition passes all enabled filters. Digital evidence is not a guarantee that a source offers that edition; some useful editions have no such evidence and can be excluded. All source records remain available in **Torrents**.

Stop any running book update before changing the filters. Saving a different selection starts a new resumable import. Existing entries outside the new selection are removed after it finishes; source records and film and series information are kept. **Statistics → Import details** distinguishes records excluded by filters from unreadable records.

After the first import, AIOSources follows Open Library's recent changes. The default update interval is **15 minutes**, and you can change it under **Schedules**. Saved progress and pending updates survive interruptions. Automatic updates must remain enabled for automatic recovery. **Metadata → Books → Settings → Advanced → Manual actions → Update book information** checks for changes immediately, while still respecting provider-requested waits.

If an interruption or a busy upstream period makes the change history impossible to cover reliably, AIOSources automatically requests a fresh export. It checks daily while waiting for an export recent enough to cover the missing period. Recent changes continue to be collected and processed; the existing catalog stays available. **Statistics** distinguishes the export date, pending updates and the date through which changes have actually been applied. A catalog awaiting recovery is not reported as up to date.

Recovery updates the existing records. Newer information obtained from the API is protected from older export records. Renamed titles, corrected editions, author changes, merges and deletions update the affected identifications. A complete recovery export also retires records outside the selection. Without catalog filters, an export containing unreadable records is not used to infer absence. With filters, entries must have a selected edition; readable information needed by selected editions is retained when an associated record cannot be decoded.

An unreadable or abnormally large metadata record is skipped and counted in the update history, so it cannot block the remaining catalog. Interrupted transfers keep their saved progress. A gzip transfer may need to be downloaded and read again up to the saved line; already committed book records are not duplicated.

Only useful bibliographic information is retained: titles, authors, editions, identifiers, languages, dates and cover references. Full descriptions, image files and the downloaded export files are not stored. Review the space used in **Administration → Storage** before enabling the complete, unfiltered catalog on a small server.

When filters remove existing rows, PostgreSQL can reuse their space. Allocated database files do not necessarily shrink immediately, so a smaller retained catalog may not immediately reduce the size reported by the operating system.

## Freshness and remaining limits

An update interval is a polling schedule, not a promise of instant availability. The first import, a backlog, network failures and provider quotas can delay updates. Open Library requests are paced across the whole instance, including change discovery and document reads. Grouped reads reduce the number of calls; missing or incompatible grouped responses fall back to individual reads. The application identifies itself honestly and does not invent contact details to obtain a higher allowance.

Open Library publishes its [full exports monthly](https://openlibrary.org/developers/dumps). Its [Recent Changes API](https://openlibrary.org/dev/docs/api/recentchanges) has bounded pagination and does not offer an immutable replication cursor. A very busy day or an interruption can make a complete bridge impossible to prove. Recovery then depends on the publication of a newer export, which can take weeks or longer if its publication is delayed. New changes still flow while this warning is present. The API is experimental; a future incompatible response stops the affected progress instead of being silently accepted.

Neither Open Library nor TMDB guarantees complete or error-free information. Missing editions, authors, identifiers, translations or cover references cannot be reconstructed reliably from nothing. Corrections become available after the provider publishes them and a successful local update applies them. Images remain remote and depend on their image services being reachable. Ambiguous identification still needs review.

TMDB has different limits: its [daily exports](https://developer.themoviedb.org/docs/daily-id-exports) provide identifiers rather than complete film and series information, so details still require API calls. Its [change lists](https://developer.themoviedb.org/docs/tracking-content-changes) cover at most 14 days, and its [request limits](https://developer.themoviedb.org/docs/rate-limiting) can change. After a longer interruption, AIOSources automatically refreshes existing film and series records progressively and resumes normal change tracking. Published information, books and manual choices remain available. An interrupted recovery continues from saved progress; already refreshed records do not need another download. A complete refresh can take a long time on a large catalog, because TMDB requires individual detail requests.

For both providers, **Statistics** shows the synchronization state, export date, next automatic update and the date through which changes have actually been applied. Finding new changes does not imply that all their details have been applied. Pending records and failed requests remain visible. Provider-requested waits survive restarts; manual actions respect them without extending their deadline. If only the change feed fails, already queued details can still be processed in bounded batches. Authentication failures and provider-requested waits remain blocking. The retry controls under **Advanced** apply to both providers. Keep automatic updates enabled for automatic recovery, or use the corresponding manual update action.

## Find and download a book

Use **Search** with a title, author or ISBN. **Type** can narrow results to Books, Ebooks or Audiobooks. A year can match an edition's publication year. Search stays local; your queries are not sent to Open Library.

Open a title to compare its source releases and debrid availability. When several formats or audio tracks are available, **Choose file** shows the files offered by the debrid service. Select the format or track you want, then choose **Download**. Uncached releases can be added to debrid when your account and source permissions allow it.

Book titles use your browser language when a suitable local edition is known, with other local title and English fallbacks. Covers load directly from Open Library; movie and series posters load from TMDB. Missing images use a placeholder.

Search works the same way on an enabled public instance, using the visitor's own debrid credentials and the instance's access rules. Private-source announces and administrator keys are never borrowed for visitor downloads.

## Check uncertain identifications

In **Identification**, open **Review**, choose **Books** and search the local catalog by title, author or ISBN. Confirm the correct work. Invalid identifiers, conflicting authors, homonyms and multi-book collections are kept for review rather than guessed.

A collection is not automatically assigned to every book it may contain. A manual choice associates that source record with the selected work. Open Library does not describe every edition; unmatched records remain accessible in **Torrents**.

## Connect another application

Create a **Torznab** access and choose its book media types as needed. Compatible clients can use book search and the standard book and audiobook categories. Stremio, Nuvio and AIOStreams installations remain for video playback.

To remove book information and its related identifications, history and availability observations, use **Metadata → Books → Settings → Advanced → Maintenance → Reset book information**. Read the confirmation first. This keeps your source records, film and series information and files in your debrid accounts. Book information must be imported again before book identifications become available.

Resets run in the background, so you can leave the page without interrupting them. **Maintenance** shows their status and **Logs** records the outcome. On a large catalog, removing related identifications and history can still take several minutes. A server shutdown interrupts the transaction rather than committing a partial reset; check its outcome before trying again. Automatic updates can import information again after a completed reset.
