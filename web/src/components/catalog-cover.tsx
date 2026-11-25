import { useState } from 'react'
import {
  AppWindowIcon,
  BookOpenIcon,
  FilmStripIcon,
  GameControllerIcon,
  MusicNotesIcon,
} from '@phosphor-icons/react'
import { cn } from '@/lib/utils'

const allowedCoverPattern =
  /^https:\/\/(?:image\.tmdb\.org\/t\/p\/|covers\.openlibrary\.org\/b\/id\/|images\.igdb\.com\/igdb\/image\/upload\/t_cover_big\/|cdn\.jsdelivr\.net\/gh\/selfhst\/icons@main\/png\/[a-z0-9][a-z0-9-]{0,127}\.png$|coverartarchive\.org\/release(?:-group)?\/[0-9a-f-]{36}\/front(?:-500|-250)?$)/

export function isAllowedCoverURL(src?: string): src is string {
  return Boolean(src && allowedCoverPattern.test(src))
}

export function CatalogCover({
  src,
  book = false,
  game = false,
  software = false,
  music = false,
  large = false,
}: {
  src?: string
  book?: boolean
  game?: boolean
  software?: boolean
  music?: boolean
  large?: boolean
}) {
  const [failed, setFailed] = useState('')
  const allowed = isAllowedCoverURL(src)
  const Icon = music
    ? MusicNotesIcon
    : software
      ? AppWindowIcon
      : game
        ? GameControllerIcon
        : book
          ? BookOpenIcon
          : FilmStripIcon
  return (
    <div
      data-slot="catalog-cover"
      className={cn(
        'flex shrink-0 items-center justify-center text-muted-foreground',
        large ? 'size-36' : 'size-18',
      )}
      aria-hidden="true"
    >
      {allowed && failed !== src ? (
        <img
          src={src}
          alt=""
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          className="block h-auto max-h-full w-auto max-w-full rounded-md border border-border bg-muted object-contain"
          onError={() => setFailed(src)}
        />
      ) : (
        <Icon size={large ? 32 : 20} />
      )}
    </div>
  )
}
