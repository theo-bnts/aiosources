import { DownloadSimpleIcon, UploadSimpleIcon, XIcon } from '@phosphor-icons/react'
import { useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, AUTO_REFRESH_INTERVAL, queryClient, refreshData, uploadBinary } from '@/lib/api'
import { prepareBrowserDownload } from '@/lib/browser-download'
import { bytes } from '@/lib/format'
import { useNotification } from '@/lib/notifications'
import { routes } from '@/lib/routes'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'

type DatabaseBackupTools = {
  export_available: boolean
  import_available: boolean
  version?: string
  reason?: string
}

type DatabaseRestoreState = {
  status: '' | 'running' | 'succeeded' | 'failed'
  phase?: 'stopping' | 'validating' | 'restoring' | 'migrating' | 'verifying' | 'finalizing'
  filename?: string
  bytes?: number
  started_at?: string
  finished_at?: string
  error?: string
}

type DatabaseBackupStatus = {
  tools: DatabaseBackupTools
  export_running: number
  restore: DatabaseRestoreState
  running: number
  indexing: boolean
  busy: boolean
}

type DatabaseExportTicket = {
  url: string
  filename: string
  expires_in: number
}

const PG_DUMP_MAGIC = 'PGDMP'

async function hasPGDumpHeader(file: File): Promise<boolean> {
  if (file.size < PG_DUMP_MAGIC.length) return false
  const header = new Uint8Array(await file.slice(0, PG_DUMP_MAGIC.length).arrayBuffer())
  return new TextDecoder('ascii').decode(header) === PG_DUMP_MAGIC
}

function asciiHeaderFilename(name: string): string {
  const cleaned = name
    .replace(/[^\x20-\x7e]+/g, '_')
    .replace(/["\\/;]+/g, '_')
    .trim()
    .slice(0, 120)
  return cleaned || 'database-backup.dump'
}

function restorePhaseMessage(restore: DatabaseRestoreState): string {
  const name = restore.filename ? ` (${restore.filename})` : ''
  switch (restore.phase) {
    case 'stopping':
      return `Stopping background work before restoring the database${name}. You can leave this page while the restore completes.`
    case 'validating':
      return `Validating backup archive${name}. You can leave this page while the restore completes.`
    case 'restoring':
      return `Restoring PostgreSQL database${name}. You can leave this page while the restore completes.`
    case 'migrating':
      return `Checking schema compatibility and applying migrations${name}. You can leave this page while the restore completes.`
    case 'verifying':
      return `Verifying encryption key compatibility${name}. You can leave this page while the restore completes.`
    case 'finalizing':
      return `Finalizing database restore${name}. You can leave this page while the restore completes.`
    default:
      return `Restoring PostgreSQL database${name}. You can leave this page while the restore completes.`
  }
}

export function DatabaseBackupPanel() {
  const location = useLocation()
  const notify = useNotification()
  const [open, setOpen] = useState(location.hash === '#database-backup')
  useEffect(() => {
    if (location.hash === '#database-backup') setOpen(true)
  }, [location.hash])

  const fileInput = useRef<HTMLInputElement>(null)
  const uploadAbort = useRef<AbortController | null>(null)
  const [pendingFile, setPendingFile] = useState<File | null>(null)
  const [fileError, setFileError] = useState<Error | null>(null)
  const [uploadState, setUploadState] = useState<{
    filename: string
    loaded: number
    total: number
  } | null>(null)

  const queryKey = ['database-backup'] as const
  const query = useQuery({
    queryKey,
    enabled: open,
    queryFn: ({ signal }) => api<DatabaseBackupStatus>('/settings/storage/database', { signal }),
    refetchInterval: (state) => {
      if (!open) return false
      return state.state.data?.restore.status === 'running' ? 1000 : AUTO_REFRESH_INTERVAL()
    },
  })

  const restoreStatus = query.data?.restore.status ?? ''
  const previousRestoreStatus = useRef<string>('')
  useEffect(() => {
    if (previousRestoreStatus.current === 'running' && restoreStatus === 'succeeded') {
      void refreshData()
      notify('Database backup restored.')
    }
    previousRestoreStatus.current = restoreStatus
  }, [restoreStatus, notify])

  const uploading = uploadState !== null
  useUnsavedChanges(uploading, 'database backup upload', uploading)

  const exportMutation = useMutation({
    mutationFn: () =>
      api<DatabaseExportTicket>('/settings/storage/database/export', {
        method: 'POST',
        body: JSON.stringify({}),
      }),
    onSuccess: (ticket) => {
      const started = prepareBrowserDownload().start(ticket.url)
      if (!started) {
        notify({
          message: 'Could not start the database backup download.',
          warning: true,
        })
        return
      }
      notify(`Database export started (${ticket.filename}).`)
      void queryClient.invalidateQueries({ queryKey })
    },
  })

  const importMutation = useMutation({
    mutationFn: async (file: File) => {
      const controller = new AbortController()
      uploadAbort.current = controller
      setUploadState({ filename: file.name, loaded: 0, total: file.size })
      try {
        return await uploadBinary<DatabaseBackupStatus>('/settings/storage/database/import', file, {
          headers: {
            'X-AIOSources-Confirm': 'replace-database',
            'X-AIOSources-Filename': asciiHeaderFilename(file.name),
          },
          signal: controller.signal,
          onProgress: (loaded, total) => {
            setUploadState({ filename: file.name, loaded, total: total || file.size })
          },
        })
      } finally {
        uploadAbort.current = null
        setUploadState(null)
      }
    },
    onSuccess: (next) => {
      setFileError(null)
      queryClient.setQueryData(queryKey, next)
      void queryClient.invalidateQueries({ queryKey })
    },
    onError: (error) => {
      if (error instanceof Error && error.name === 'AbortError') {
        notify('Database backup upload cancelled.')
        return
      }
      void queryClient.invalidateQueries({ queryKey })
    },
  })

  const status = query.data
  const restoring = status?.restore.status === 'running'

  let exportDisabledReason: string | undefined
  if (query.error) {
    exportDisabledReason = 'Database backup availability could not be checked. Try again above.'
  } else if (!status) {
    exportDisabledReason = 'Wait for database backup status to load.'
  } else if (!status.tools.export_available) {
    exportDisabledReason = status.tools.reason || 'pg_dump is unavailable on this server.'
  } else if (restoring || uploading) {
    exportDisabledReason = 'Wait for the active database import to finish before exporting.'
  } else if (exportMutation.isPending) {
    exportDisabledReason = 'Preparing database export.'
  }

  let importDisabledReason: string | undefined
  if (query.error) {
    importDisabledReason = 'Database backup availability could not be checked. Try again above.'
  } else if (!status) {
    importDisabledReason = 'Wait for database backup status to load.'
  } else if (!status.tools.import_available) {
    importDisabledReason = status.tools.reason || 'pg_restore is unavailable on this server.'
  } else if (uploading || restoring) {
    importDisabledReason = 'A database import is already in progress.'
  } else if (status.export_running > 0) {
    importDisabledReason = 'Wait for the active database export to finish before importing.'
  } else if (status.running > 0) {
    importDisabledReason = 'Pause running imports before importing a database backup.'
  } else if (status.indexing) {
    importDisabledReason = 'Wait for search index changes to finish before importing a backup.'
  } else if (status.busy) {
    importDisabledReason =
      'Wait for active metadata, identification or maintenance work to finish before importing a backup.'
  }

  const sharedStatusMessage =
    exportDisabledReason && importDisabledReason && exportDisabledReason === importDisabledReason
      ? exportDisabledReason
      : undefined
  const uploadPercent =
    uploadState && uploadState.total > 0
      ? Math.min(100, Math.round((uploadState.loaded / uploadState.total) * 100))
      : 0

  return (
    <Disclosure
      id="database-backup"
      title="Database backup"
      open={open}
      onOpenChange={setOpen}
      help={
        <>
          <p>
            Export streams a compressed PostgreSQL custom archive (<code>pg_dump -Fc</code>) of the
            AIOSources database without stopping reads or buffering the dump to disk.
          </p>
          <p>
            Import replaces the entire AIOSources PostgreSQL schema using <code>pg_restore</code>,
            validates schema compatibility and encryption key matching, and rolls back automatically
            if restoration fails.
          </p>
          <p>
            Keep backups private. Database backups contain encrypted secrets; keep the matching
            encryption key (<code>master-key</code> / <code>MASTER_KEY</code>) and source definition
            files alongside your backups.
          </p>
        </>
      }
    >
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        open && <Loading label="Loading database backup controls" />
      ) : (
        <div className="space-y-4">
          <input
            ref={fileInput}
            type="file"
            accept=".dump,application/octet-stream"
            className="sr-only"
            aria-label="Choose a PostgreSQL database backup file"
            onChange={async (event) => {
              const file = event.target.files?.[0]
              event.target.value = ''
              if (!file) return
              setFileError(null)
              importMutation.reset()
              try {
                if (!(await hasPGDumpHeader(file))) {
                  setFileError(
                    new Error(
                      'The selected file is not a PostgreSQL custom backup archive (.dump).',
                    ),
                  )
                  return
                }
                setPendingFile(file)
              } catch {
                setFileError(new Error('Could not read the selected backup file.'))
              }
            }}
          />

          <div className="toolbar">
            <div>
              <Button
                id="database-backup-export"
                type="button"
                variant="outline"
                disabled={Boolean(exportDisabledReason)}
                aria-describedby={exportDisabledReason ? 'database-export-status' : undefined}
                onClick={() => {
                  setFileError(null)
                  exportMutation.mutate()
                }}
              >
                <DownloadSimpleIcon aria-hidden="true" />
                {exportMutation.isPending ? 'Preparing export…' : 'Export database'}
              </Button>
            </div>
            <div>
              <Button
                id="database-backup-import"
                type="button"
                variant="outline"
                disabled={Boolean(importDisabledReason)}
                aria-describedby={
                  sharedStatusMessage
                    ? 'database-export-status'
                    : importDisabledReason
                      ? 'database-import-status'
                      : undefined
                }
                onClick={() => {
                  setFileError(null)
                  fileInput.current?.click()
                }}
              >
                <UploadSimpleIcon aria-hidden="true" />
                {uploading ? 'Uploading…' : restoring ? 'Restoring…' : 'Import database'}
              </Button>
            </div>
          </div>

          {status?.tools.version && (
            <p className="help">
              Native PostgreSQL archive format ({status.tools.version}, compressed custom dump).
            </p>
          )}

          {sharedStatusMessage ? (
            <p
              id="database-export-status"
              className="help text-warning-foreground"
              role="status"
              aria-live="polite"
            >
              {sharedStatusMessage}
            </p>
          ) : (
            <>
              <p
                id="database-export-status"
                className={exportDisabledReason ? 'help text-warning-foreground' : 'sr-only'}
                role="status"
                aria-live="polite"
              >
                {exportDisabledReason}
              </p>
              <p
                id="database-import-status"
                className={importDisabledReason ? 'help text-warning-foreground' : 'sr-only'}
                role="status"
                aria-live="polite"
              >
                {importDisabledReason}
              </p>
            </>
          )}

          {uploadState && (
            <div className="space-y-2" role="status" aria-live="polite">
              <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
                <span>
                  Uploading {uploadState.filename} ({uploadPercent}% · {bytes(uploadState.loaded)}{' '}
                  of {bytes(uploadState.total)})…
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => uploadAbort.current?.abort()}
                >
                  <XIcon aria-hidden="true" />
                  Cancel upload
                </Button>
              </div>
              <div className="h-1.5 w-full max-w-md overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full bg-primary transition-[width] duration-150"
                  style={{ width: `${uploadPercent}%` }}
                />
              </div>
            </div>
          )}

          {status?.restore.status === 'running' && (
            <p role="status" className="help">
              {restorePhaseMessage(status.restore)}{' '}
              <Link to={routes.administration.logs} className="data-link">
                View logs
              </Link>
            </p>
          )}

          {status?.restore.status === 'succeeded' && !uploading && (
            <p role="status" className="help">
              Database backup restored
              {status.restore.filename ? ` from ${status.restore.filename}` : ''}.{' '}
              <Link to={routes.administration.logs} className="data-link">
                View logs
              </Link>
            </p>
          )}

          <ErrorNotice error={exportMutation.error} />
          <ErrorNotice error={fileError} />
          <ErrorNotice
            error={
              importMutation.error instanceof Error && importMutation.error.name === 'AbortError'
                ? null
                : importMutation.error
            }
          />
          {status?.restore.status === 'failed' && status.restore.error && !uploading && (
            <ErrorNotice error={status.restore.error} />
          )}

          <AlertDialog
            open={pendingFile !== null}
            onOpenChange={(nextOpen) => {
              if (!nextOpen) setPendingFile(null)
            }}
          >
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Import database backup?</AlertDialogTitle>
                <AlertDialogDescription>
                  {pendingFile && (
                    <>
                      This replaces the entire AIOSources PostgreSQL database (torrents, catalog
                      metadata, identifications, settings, history, users and shares) using{' '}
                      <span className="font-medium text-foreground">{pendingFile.name}</span> (
                      {bytes(pendingFile.size)}). Source definition files on disk are kept. If the
                      backup contains encrypted secrets, this instance must use the matching master
                      key. Your current administrator session stays signed in, and the previous
                      database is restored automatically if validation or restoration fails. This
                      cannot be undone once completed.
                    </>
                  )}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Back</AlertDialogCancel>
                <Button
                  type="button"
                  variant="destructive"
                  onClick={() => {
                    const selected = pendingFile
                    setPendingFile(null)
                    if (selected) importMutation.mutate(selected)
                  }}
                >
                  <UploadSimpleIcon aria-hidden="true" />
                  Replace database
                </Button>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      )}
    </Disclosure>
  )
}
