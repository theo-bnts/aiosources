# Books and audiobooks

[Back to README](../README.md) · [Search and identification](OPERATIONS.md) · [Torznab](TORZNAB.md)

AIOSources can identify ebooks, comics and audiobooks already present in your imported sources, then offer them through Search and Torznab. It does not supply book sources or download books from Open Library.

## Set up book information

1. Import your authorized sources as usual. Keep useful fields such as the title, categories, author and ISBN when your source supplies them. Existing imported titles do not need to be downloaded again.
2. Open **Metadata → Settings → Schedules** and enable **Automatically update book information**. No Open Library account or API key is required.
3. Allow the first complete catalog import to finish. It can take several hours or longer and needs substantial additional database storage. The complete export contains many gigabytes even before it is imported; this option is off by default.
4. Enable automatic identification, or use **Advanced → Manual actions → Identify torrents** after the book information import finishes.

After the first import, AIOSources follows Open Library's recent changes. The default update interval is **15 minutes**, and you can change it under **Schedules**. Saved progress and pending updates survive interruptions. Automatic updates must remain enabled for automatic recovery. You can also run **Advanced → Manual actions → Update book information** immediately.

If an interruption or a busy upstream period makes the change history impossible to cover reliably, AIOSources automatically requests a fresh export. It checks daily while waiting for an export recent enough to cover the missing period. Recent changes continue to be collected and processed; the existing catalog stays available. **Statistics** distinguishes the export date, pending updates and the date through which changes have actually been applied. A catalog awaiting recovery is not reported as up to date.

Recovery updates the existing records. Newer information obtained from the API is protected from older export records. Renamed titles, corrected editions, author changes, merges and deletions update the affected identifications. A complete, readable recovery export also retires records no longer present in that export; an export containing unreadable records is not used to infer absence.

An unreadable or abnormally large metadata record is skipped and counted in the update history, so it cannot block the remaining catalog. Interrupted transfers keep their saved progress. A gzip transfer may need to be downloaded and read again up to the saved line; already committed book records are not duplicated.

Only useful bibliographic information is retained: titles, authors, editions, identifiers, languages, dates and cover references. Full descriptions and image files are not stored. Review the space used in **Administration → Storage** before enabling the full catalog on a small server.

## Freshness and remaining limits

An update interval is a polling schedule, not a promise of instant availability. The first import, a backlog, network failures and provider quotas can delay updates. Open Library requests are paced across the whole instance, including change discovery and document reads. Grouped reads reduce the number of calls; missing or incompatible grouped responses fall back to individual reads. The application identifies itself honestly and does not invent contact details to obtain a higher allowance.

Open Library publishes its [full exports monthly](https://openlibrary.org/developers/dumps). Its [Recent Changes API](https://openlibrary.org/dev/docs/api/recentchanges) has bounded pagination and does not offer an immutable replication cursor. A very busy day or an interruption can make a complete bridge impossible to prove. Recovery then depends on the publication of a newer export, which can take weeks or longer if its publication is delayed. New changes still flow while this warning is present. The API is experimental; a future incompatible response stops the affected progress instead of being silently accepted.

Neither Open Library nor TMDB guarantees complete or error-free information. Missing editions, authors, identifiers, translations or cover references cannot be reconstructed reliably from nothing. Corrections become available after the provider publishes them and a successful local update applies them. Images remain remote and depend on their image services being reachable. Ambiguous identification still needs review.

TMDB has different limits: its [daily exports](https://developer.themoviedb.org/docs/daily-id-exports) provide identifiers rather than complete film and series information, so details still require API calls. Its [change lists](https://developer.themoviedb.org/docs/tracking-content-changes) cover at most 14 days, and its [request limits](https://developer.themoviedb.org/docs/rate-limiting) can change. After a longer interruption, AIOSources automatically refreshes existing film and series records progressively and resumes normal change tracking. Published information, books and manual choices remain available. An interrupted recovery continues from saved progress; already refreshed records do not need another download. A complete refresh can take a long time on a large catalog, because TMDB requires individual detail requests.

For both providers, **Statistics** shows the synchronization state, export date, next automatic update and the date through which changes have actually been applied. Finding new changes does not imply that all their details have been applied. Pending records and failed requests remain visible. Provider-requested waits survive restarts; manual actions respect them too. Keep automatic updates enabled for automatic recovery, or use the corresponding manual update action.

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

To remove only the book information, use **Metadata → Settings → Advanced → Maintenance → Reset book information**. Read the confirmation first. This keeps your source records, manual choices and film and series information; book information must be imported again before those book matches are available.
