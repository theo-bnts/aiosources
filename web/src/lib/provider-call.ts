export const providerSteps: Record<string, string> = {
  provider_request: 'Provider request',
  provider_availability: 'Check availability',
  provider_inventory: 'Check account torrents',
  provider_add: 'Prepare torrent',
  provider_files: 'Read files',
  provider_cleanup: 'Remove temporary torrent',
  provider_unlock: 'Resolve download link',
  provider_playback: 'Resolve playback link',
}
export const providerErrors: Record<string, string> = {
  provider_error: 'Provider error',
  authentication: 'Key or subscription rejected',
  cancelled: 'Interrupted',
  timeout: 'Timed out',
}
