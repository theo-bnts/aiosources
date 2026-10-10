# Content, service terms and attribution

## Software license

AIOSources is distributed under the [MIT License](LICENSE), including the preserved copyright of Ingest and its contributors. The Torznab SDK retains that [license](torznab/LICENSE). Third-party libraries, fonts, icons and the image’s CA certificate bundle retain their original terms, reproduced in [THIRD-PARTY-NOTICES.txt](web/public/THIRD-PARTY-NOTICES.txt) and distributed with the application image.

The software license grants rights to the software. It does not grant rights to third-party catalogs, metadata, media, trademarks, service accounts or API access. Commercial use of the software and permission to use a particular service or dataset are separate questions.

The unmodified Mozilla certificate-store component is governed by MPL-2.0, separately from the MIT application. Its full license, original notice and verified source-archive URL are included in the distributed notices. The pinned image’s certificate package is reviewed when its builder changes.

## Sources and content

AIOSources is a configurable tool for authorized source collection, metadata identification and playback integration. It contains no configured content sources, directory of source sites, private source credentials, media files or default announce addresses. Documentation and network fixtures use fictitious endpoints or local test servers. Users supply their own source definitions and credentials.

Use public-domain material, openly licensed content, your own content or other material for which you have the necessary permissions. Only collect, access and share source information when its owner and applicable terms authorize you to do so. Availability, a successful identifier match or a shared link does not establish permission to use or redistribute content.

Source configuration remains private administrator data. Contributors must not submit real content-site definitions, account credentials, production captures or unauthorized material; see [Contributing](CONTRIBUTING.md). The application does not attempt to determine the rights attached to every user-supplied record.

## Service accounts

Use your own authorized accounts and follow each provider's current terms, quotas and account-sharing restrictions. AIOSources does not resell service access, provide accounts or override provider permissions. Operators are responsible for access they grant through an installation link or catalog share.

- [AllDebrid terms](https://alldebrid.com/tos/)
- [TorBox terms](https://torbox.app/terms) and [API/developer terms](https://torbox.app/policies/api-developer-terms)
- [TMDB API usage and attribution](https://developer.themoviedb.org/docs/faq)

Saving or validating a source does not contact it. Starting an import or enabling its schedule authorizes source requests. Metadata updates contact TMDB. Availability and selected playback requests contact the configured debrid services and can create account entries. Credentials belong in encrypted Secrets; installation and sharing links must be protected like credentials.

## TMDB attribution

This product uses the TMDB API but is not endorsed or certified by TMDB.

The approved TMDB logo and notice appear in the application's **About AIOSources** credits. The logo is bundled unchanged from TMDB's [official attribution page](https://www.themoviedb.org/about/logos-attribution):

```text
Asset: blue_short-8e7b30f73a4020692ccca9c88bafe5dcb6f8a62a4c6bc55cd9ba82bb2cd95f6c.svg
SHA-256: 8e7b30f73a4020692ccca9c88bafe5dcb6f8a62a4c6bc55cd9ba82bb2cd95f6c
```

TMDB's logo remains subject to its branding requirements and is not relicensed under MIT. TMDB API access and data use have their own terms, including separate licensing for commercial use. No film posters, backdrops or catalog export are bundled with AIOSources.

## Scope of the public review

The public review covers checked-in source, examples, assets, dependency notices and the shipped application. It is not a certification of every possible deployment, user-supplied catalog or jurisdiction. Removing a Git history also does not erase other people's copies or guarantee immediate removal of GitHub's internal cached objects; see [GitHub's documented removal limits](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository).
