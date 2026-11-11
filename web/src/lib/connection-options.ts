export const stremioMediaTypes = [
  { value: 'movie', label: 'Movies', group: 'Videos' },
  { value: 'series', label: 'Series', group: 'Videos' },
  { value: 'anime', label: 'Anime', group: 'Videos' },
]

export const torznabMediaTypes = [
  { value: 'movie', label: 'Movies', group: 'Videos' },
  { value: 'tv', label: 'Series', group: 'Videos' },
  { value: 'game', label: 'Games', group: 'Games' },
  { value: 'ebook', label: 'Ebooks', group: 'Books' },
  { value: 'audiobook', label: 'Audiobooks', group: 'Books' },
  { value: 'book', label: 'Other books', group: 'Books' },
  { value: 'other', label: 'Other / not identified' },
]

export const catalogMediaTypes = torznabMediaTypes.filter((option) => option.value !== 'other')

// Review chooses a metadata work, independently of any release's book format.
export const reviewMediaTypes = [
  { value: 'movie', label: 'Movies', group: 'Videos' },
  { value: 'tv', label: 'Series', group: 'Videos' },
  { value: 'game', label: 'Games', group: 'Games' },
  { value: 'book', label: 'Books', group: 'Books' },
]
