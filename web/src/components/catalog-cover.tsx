import { useState } from 'react'
import { BookOpenIcon, FilmStripIcon, GameControllerIcon } from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

export function CatalogCover({
  src,
  book = false,
  game = false,
  large = false,
}: {
  src?: string
  book?: boolean
  game?: boolean
  large?: boolean
}) {
  const [failed, setFailed] = useState('')
  const allowed =
    src &&
    /^https:\/\/(?:image\.tmdb\.org\/t\/p\/|covers\.openlibrary\.org\/b\/id\/|images\.igdb\.com\/igdb\/image\/upload\/t_cover_big\/)/.test(
      src,
    )
  const Icon = game ? GameControllerIcon : book ? BookOpenIcon : FilmStripIcon
  return (
    <div
      data-slot="catalog-cover"
      className={cn(
        'flex shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted text-muted-foreground',
        large ? 'size-36' : 'size-18',
      )}
      aria-hidden="true"
    >
      {allowed && failed !== src ? (
        <img
          src={src}
          alt=""
          width={large ? 144 : 72}
          height={large ? 144 : 72}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          className="h-full w-full object-contain"
          onError={() => setFailed(src)}
        />
      ) : (
        <Icon size={large ? 32 : 20} />
      )}
    </div>
  )
}
