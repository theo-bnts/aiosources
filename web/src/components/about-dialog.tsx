import {
  ArrowUpRightIcon,
  DatabaseIcon,
  DiscordLogoIcon,
  GithubLogoIcon,
  HeartIcon,
  InfoIcon,
} from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'

export function AboutDialog() {
  const embeddedVersion = document.querySelector<HTMLMetaElement>(
    'meta[name="application-version"]',
  )?.content
  const version =
    embeddedVersion && embeddedVersion !== '__AIOSOURCES_VERSION__'
      ? embeddedVersion.slice(0, 200)
      : 'dev'
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="size-11 bg-background sm:w-auto lg:h-9 dark:bg-background"
          aria-label="About AIOSources"
          title="About AIOSources"
        >
          <InfoIcon />
          <span className="hidden sm:inline">About</span>
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex min-w-0 items-center gap-2">
            <DatabaseIcon size={24} className="shrink-0 text-primary" aria-hidden="true" />
            <span className="min-w-0 break-words">
              AIOSources{' '}
              <span className="text-sm font-normal text-muted-foreground">({version})</span>
            </span>
          </DialogTitle>
          <DialogDescription>
            Bring your sources together for search, playback and downloads.
          </DialogDescription>
        </DialogHeader>
        <section aria-label="Project and support" className="grid gap-3 sm:grid-cols-2">
          <Button variant="outline" className="h-auto justify-start gap-3 px-4 py-3" asChild>
            <a
              href="https://github.com/theo-bnts/aiosources"
              target="_blank"
              rel="noopener noreferrer"
            >
              <GithubLogoIcon aria-hidden="true" />
              <span className="grid gap-1 text-left">
                <span>GitHub</span>
                <span className="help font-normal">Source code and releases</span>
              </span>
              <ArrowUpRightIcon className="ml-auto shrink-0" aria-hidden="true" />
            </a>
          </Button>
          <Button variant="outline" className="h-auto justify-start gap-3 px-4 py-3" asChild>
            <a href="https://discord.gg/h3sja5XN8b" target="_blank" rel="noopener noreferrer">
              <DiscordLogoIcon aria-hidden="true" />
              <span className="grid gap-1 text-left">
                <span>Discord</span>
                <span className="help font-normal">Help in French only</span>
              </span>
              <ArrowUpRightIcon className="ml-auto shrink-0" aria-hidden="true" />
            </a>
          </Button>
          <Button
            variant="outline"
            className="h-auto justify-start gap-3 px-4 py-3 sm:col-span-2"
            asChild
          >
            <a
              href="https://github.com/sponsors/theo-bnts"
              target="_blank"
              rel="noopener noreferrer"
            >
              <HeartIcon aria-hidden="true" />
              <span className="grid gap-1 text-left">
                <span>Support AIOSources</span>
                <span className="help font-normal">GitHub Sponsors</span>
              </span>
              <ArrowUpRightIcon className="ml-auto shrink-0" aria-hidden="true" />
            </a>
          </Button>
        </section>
        <p className="text-sm leading-6 text-muted-foreground">
          Use sources you are authorized to access and content you are entitled to use and share.
          Source definitions and service accounts are configured by you.
        </p>
        <section aria-label="Credits" className="space-y-3 border-t pt-4">
          <div className="flex items-start gap-3">
            <a
              href="https://www.themoviedb.org"
              target="_blank"
              rel="noopener noreferrer"
              className="mt-1 shrink-0"
            >
              <img src="/tmdb.svg" alt="TMDB" className="h-4 w-auto" />
            </a>
            <p className="text-sm leading-6 text-muted-foreground">
              This product uses the TMDB API but is not endorsed or certified by TMDB.
            </p>
          </div>
          <p className="text-sm leading-6 text-muted-foreground">
            Book information and covers:{' '}
            <a
              href="https://openlibrary.org"
              target="_blank"
              rel="noopener noreferrer"
              className="data-link"
            >
              Open Library
            </a>
            .
          </p>
          <a
            href="/THIRD-PARTY-NOTICES.txt"
            target="_blank"
            rel="noopener noreferrer"
            className="data-link text-sm"
          >
            Software licenses and notices
          </a>
        </section>
      </DialogContent>
    </Dialog>
  )
}
