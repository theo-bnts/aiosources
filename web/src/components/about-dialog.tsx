import {
  ArrowUpRightIcon,
  DatabaseIcon,
  DiscordLogoIcon,
  GithubLogoIcon,
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
          <DialogTitle className="flex items-center gap-2">
            <DatabaseIcon size={24} className="shrink-0 text-primary" aria-hidden="true" />
            AIOSources
          </DialogTitle>
          <DialogDescription>
            Your sources, one searchable catalog, ready for Stremio.
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
        </section>
        <p className="text-sm leading-6 text-muted-foreground">
          Use sources you are authorized to access and content you are entitled to use and share.
          Source definitions and service accounts are configured by you.
        </p>
        <section aria-label="Credits" className="space-y-4 border-t pt-4">
          <div className="space-y-2">
            <a
              href="https://www.themoviedb.org"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-block"
            >
              <img src="/tmdb.svg" alt="TMDB" className="h-4 w-auto" />
            </a>
            <p className="text-sm leading-6 text-muted-foreground">
              This product uses the TMDB API but is not endorsed or certified by TMDB.
            </p>
          </div>
          <p className="help">
            Built on{' '}
            <a
              href="https://github.com/moodiness/ingest"
              target="_blank"
              rel="noopener noreferrer"
              className="data-link"
            >
              Ingest
            </a>
            . Original copyrights and third-party licenses are preserved.
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
