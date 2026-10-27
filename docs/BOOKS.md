# Books and audiobooks

[Back to README](../README.md) · [Search and identification](OPERATIONS.md) · [Torznab](TORZNAB.md)

AIOSources can identify ebooks, comics and audiobooks already present in your imported sources, then offer them through Search and Torznab. It does not supply book sources or download books from Open Library.

## Set up book information

1. Import your authorized sources as usual. Keep useful fields such as the title, categories, author and ISBN when your source supplies them. Existing imported titles do not need to be downloaded again.
2. Open **Metadata → Settings → Schedules** and enable **Automatically update book information**. No Open Library account or API key is required.
3. Allow the first complete catalog import to finish. It can take several hours or longer and needs substantial additional database storage. The complete export contains many gigabytes even before it is imported; this option is off by default.
4. Enable automatic identification, or use **Advanced → Manual actions → Identify torrents** after the book information import finishes.

The default book update interval is 30 days, matching Open Library's monthly exports. You can change it. Saved import progress survives interruptions; automatic updates must remain enabled for automatic recovery. You can also retry from **Advanced → Manual actions → Update book information**.

An unreadable or abnormally large metadata record is skipped and counted in the update history, so it cannot block the remaining catalog. Interrupted transfers keep their saved progress.

Only useful bibliographic information is retained: titles, authors, editions, identifiers, languages, dates and cover references. Full descriptions and image files are not stored. Review the space used in **Administration → Storage** before enabling the full catalog on a small server.

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
