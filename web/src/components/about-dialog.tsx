import { DatabaseIcon, InfoIcon } from '@phosphor-icons/react'
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
        <Button variant="ghost" className="min-h-11 w-full lg:min-h-9">
          <InfoIcon />
          About AIOSources
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-3">
            <DatabaseIcon size={28} className="text-primary" aria-hidden="true" />
            AIOSources
          </DialogTitle>
          <DialogDescription>
            Your sources, one searchable catalog, ready for Stremio.
          </DialogDescription>
        </DialogHeader>
        <p className="text-sm">
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
            <p className="text-sm">
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
