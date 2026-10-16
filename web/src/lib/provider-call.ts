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
  rate_limit: 'Request rate limit reached',
  account_limit: 'Torrent or download limit reached',
  subscription: 'Subscription or higher plan required',
  provider_unavailable: 'Provider temporarily unavailable',
  invalid_torrent: 'Torrent rejected',
  file_too_large: 'File exceeds provider or plan limit',
  not_found: 'Torrent or file unavailable',
  not_cached: 'File no longer cached',
}
