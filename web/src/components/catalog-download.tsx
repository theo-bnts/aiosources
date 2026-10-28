import { serviceNames } from '@/lib/debrid'
import { useEffect, useRef, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { DownloadSimpleIcon, PlusIcon, ArrowRightIcon } from '@phosphor-icons/react'
import { api, ApiError, queryClient } from '@/lib/api'
import { usePublicSearch } from '@/lib/public-search'
import { publicAPI } from '@/lib/public-credentials'
import { bytes } from '@/lib/format'
import { useNotification } from '@/lib/notifications'
import { prepareBrowserDownload } from '@/lib/browser-download'
import { useListViewState } from '@/lib/detail-navigation'
import type {
  CatalogRelease,
  CatalogSelection,
  CatalogAvailability,
  CatalogDownload,
} from '@/lib/catalog-search-types'
import { ErrorNotice } from '@/components/common'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

const cacheLabels = {
  cached: 'Cached',
  uncached: 'Not cached',
  unknown: 'Cache unverified',
  unavailable: 'No matching file',
  invalid: 'No valid info hash',
}

export function DownloadAction({
  workID,
  release,
  service,
  selection,
  operationID,
  preparing = false,
}: {
  workID: string
  release: CatalogRelease
  service: CatalogAvailability
  selection: CatalogSelection
  operationID?: string
  preparing?: boolean
}) {
  const visitor = usePublicSearch()
  const listState = useListViewState()
  const preparation = useRef('')
  const preparationOperation = useRef(operationID ?? '')
  const [ready, setReady] = useState<CatalogDownload | null>(null)
  const [bookFiles, setBookFiles] = useState<Array<{ path: string; size: number }>>([])
  const [selectedFile, setSelectedFile] = useState(selection.file ?? '')
  const fileSelection = { ...selection, ...(selectedFile ? { file: selectedFile } : {}) }
  const [waiting, setWaiting] = useState(preparing)
  const [notReady, setNotReady] = useState(false)
  const [operation, setOperation] = useState(operationID ?? '')
  const [checkVersion, setCheckVersion] = useState(0)
  const notify = useNotification()
  const attempts = useRef(0)
  const controller = useRef<AbortController | null>(null)
  const download = useRef<ReturnType<typeof prepareBrowserDownload> | null>(null)
  useEffect(
    () => () => {
      controller.current?.abort()
      download.current?.cancel()
    },
    [],
  )
  const mutation = useMutation({
    gcTime: 0,
    mutationFn: (allowUncached: boolean) => {
      controller.current = new AbortController()
      if (visitor) {
        return publicAPI<CatalogDownload>(
          `/catalog/${workID}/download`,
          visitor.configuration,
          {
            method: 'POST',
            signal: controller.current.signal,
            body: JSON.stringify(
              preparation.current && !allowUncached
                ? { preparation_id: preparation.current, file: selectedFile || undefined }
                : {
                    ...fileSelection,
                    download_token: release.download_token,
                    service: service.service,
                    allow_uncached: allowUncached,
                  },
            ),
          },
          visitor.credential,
        )
      }
      const continuation = operationID || (!allowUncached ? preparationOperation.current : '')
      return api<CatalogDownload>(
        continuation
          ? `/operations/${encodeURIComponent(continuation)}/download`
          : `/catalog/${workID}/download`,
        {
          method: 'POST',
          signal: controller.current.signal,
          body: JSON.stringify(
            continuation
              ? { file: selectedFile || undefined }
              : {
                  ...fileSelection,
                  occurrence_id: release.occurrence_id,
                  fingerprint: release.fingerprint,
                  service: service.service,
                  allow_uncached: allowUncached,
                },
          ),
        },
      )
    },
    onSuccess: (result, allowUncached) => {
      preparation.current = result.preparation_id || ''
      preparationOperation.current = result.operation_id
      setOperation(result.operation_id)
      const pending = result.status === 'pending'
      const choosing = Boolean(result.files?.length)
      if (choosing) setBookFiles(result.files!)
      setWaiting(pending && !choosing && (allowUncached || waiting))
      setNotReady(pending && !choosing)
      if (!pending) {
        setReady(result)
        const started = result.url && download.current?.start(result.url)
        if (!started) download.current?.cancel()
        download.current = null
        notify(
          started
            ? 'The download link is ready.'
            : 'The download link is ready. Use Download file to start it.',
        )
      } else if (!(allowUncached || waiting) || attempts.current >= 20) {
        download.current?.cancel()
        download.current = null
      }
      // Keep the temporary URL only in this view's expiring local state.
      mutation.reset()
      // A fast pending response can batch both mutation transitions into one
      // render. Schedule the next check even when its visible state is unchanged.
      setCheckVersion((version) => version + 1)
      if (!visitor)
        void queryClient.invalidateQueries({ queryKey: ['operation-history', 'debrid'] })
      if (operationID) {
        void queryClient.invalidateQueries({ queryKey: ['operation', operationID] })
        void queryClient.invalidateQueries({ queryKey: ['operation-events', operationID] })
      }
    },
    onError: (error) => {
      download.current?.cancel()
      download.current = null
      if (
        error instanceof ApiError &&
        ['not_found', 'not_cached', 'conflict'].includes(error.code || '')
      ) {
        setWaiting(false)
        setNotReady(true)
        setReady(null)
        preparation.current = ''
        // Keep the old history link, but never retry a removed preparation.
        preparationOperation.current = ''
      }
      if (!visitor)
        void queryClient.invalidateQueries({ queryKey: ['operation-history', 'debrid'] })
      if (operationID) {
        void queryClient.invalidateQueries({ queryKey: ['operation', operationID] })
        void queryClient.invalidateQueries({ queryKey: ['operation-events', operationID] })
      }
    },
  })
  const run = mutation.mutate
  useEffect(() => {
    if (!waiting || mutation.isPending || mutation.isError || attempts.current >= 20) return
    const timer = window.setTimeout(() => {
      attempts.current++
      run(false)
    }, 15_000)
    return () => window.clearTimeout(timer)
  }, [waiting, mutation.isPending, mutation.isError, run, checkVersion])
  useEffect(() => {
    if (!ready) return
    const seconds = ready.url_valid_seconds ?? 600
    const delay = Number.isFinite(seconds) ? Math.max(0, Math.min(seconds, 3600) - 1) * 1000 : 0
    const timer = window.setTimeout(() => setReady(null), delay)
    return () => window.clearTimeout(timer)
  }, [ready])
  const details = [service.filename, service.size ? bytes(service.size) : null]
    .filter(Boolean)
    .join(' · ')
  const chooseFile = bookFiles.length > 1
  const fileID = `book-file-${workID}-${release.occurrence_id}-${service.service}`
  const blocked = service.state === 'invalid' || service.state === 'unavailable'
  const cached = service.state === 'cached' && !notReady
  return (
    <div className="w-full min-w-0 space-y-2 sm:min-w-48">
      {chooseFile && (
        <div className="field min-w-0">
          <Label htmlFor={fileID}>File</Label>
          <select
            id={fileID}
            className="native-select w-full max-w-full"
            value={selectedFile}
            disabled={mutation.isPending || waiting}
            onChange={(event) => {
              setSelectedFile(event.target.value)
              setReady(null)
            }}
          >
            <option value="">Choose a format or audio track</option>
            {bookFiles.map((file) => (
              <option key={file.path} value={file.path}>
                {file.path}
                {file.size > 0 ? ` · ${bytes(file.size)}` : ''}
              </option>
            ))}
          </select>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium">{serviceNames[service.service]}</span>
        <Badge variant={ready || cached ? 'success' : waiting ? 'info' : 'secondary'}>
          {ready
            ? 'Ready'
            : waiting
              ? 'Preparing'
              : notReady
                ? 'Not ready'
                : cacheLabels[service.state]}
        </Badge>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {ready?.url ? (
          <Button
            size="sm"
            title={details || undefined}
            onClick={() => prepareBrowserDownload().start(ready.url!)}
          >
            <DownloadSimpleIcon />
            Download file
          </Button>
        ) : (
          <Button
            size="sm"
            title={details || undefined}
            variant={cached ? 'default' : 'outline'}
            disabled={
              mutation.isPending ||
              (chooseFile && !selectedFile) ||
              blocked ||
              (!service.can_add && service.state === 'uncached' && !waiting)
            }
            onClick={() => {
              attempts.current = 0
              download.current?.cancel()
              download.current = prepareBrowserDownload()
              mutation.mutate(
                !waiting &&
                  !cached &&
                  service.can_add &&
                  !preparation.current &&
                  !preparationOperation.current,
              )
            }}
          >
            {cached || waiting || (chooseFile && selectedFile) ? (
              <DownloadSimpleIcon />
            ) : (
              <PlusIcon />
            )}
            {mutation.isPending
              ? 'Preparing…'
              : waiting
                ? 'Check readiness'
                : cached || (chooseFile && selectedFile)
                  ? service.file_count && service.file_count > 1 && !selectedFile
                    ? 'Choose file'
                    : 'Download'
                  : service.can_add
                    ? 'Add to debrid'
                    : 'Check cached file'}
          </Button>
        )}
        {!visitor && operation && !operationID && (
          <Button asChild size="sm" variant="ghost">
            <Link to={`/debrid/history/${encodeURIComponent(operation)}`} state={listState}>
              View operation
              <ArrowRightIcon />
            </Link>
          </Button>
        )}
      </div>
      {waiting && (
        <p className="help max-w-60">
          The debrid service is preparing this file.
          {attempts.current < 20 && !mutation.isError
            ? ' Readiness is checked automatically.'
            : ' Check readiness again later.'}
        </p>
      )}
      {notReady && !waiting && (
        <p className="help max-w-60">
          The file is not ready at this debrid service.
          {service.can_add
            ? ' You can add it to debrid.'
            : operationID
              ? ' Return to Search to choose an available version.'
              : ' Only cached files can be downloaded with this access.'}
        </p>
      )}
      <ErrorNotice error={mutation.error} scope="action" />
    </div>
  )
}
