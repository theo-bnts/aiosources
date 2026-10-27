import { useState } from 'react'
import { BookOpenIcon, FilmStripIcon } from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

export function CatalogCover({
  src,
  book = false,
  large = false,
}: {
  src?: string
  book?: boolean
  large?: boolean
}) {
  const [failed, setFailed] = useState('')
  const allowed =
    src && /^https:\/\/(?:image\.tmdb\.org\/t\/p\/|covers\.openlibrary\.org\/b\/id\/)/.test(src)
  const Icon = book ? BookOpenIcon : FilmStripIcon
  return (
    <div
      className={cn(
        'flex shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted text-muted-foreground',
        large ? 'h-36 w-24' : 'h-18 w-12',
      )}
      aria-hidden="true"
    >
      {allowed && failed !== src ? (
        <img
          src={src}
          alt=""
          width={large ? 96 : 48}
          height={large ? 144 : 72}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          className="h-full w-full object-cover"
          onError={() => setFailed(src)}
        />
      ) : (
        <Icon size={large ? 32 : 20} />
      )}
    </div>
  )
}
