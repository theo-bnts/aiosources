import {
  BellIcon,
  FloppyDiskIcon,
  GearSixIcon,
  MagnifyingGlassIcon,
  PaperPlaneTiltIcon,
  PencilSimpleIcon,
  PlusIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'

import { api, AUTO_REFRESH_INTERVAL, ConnectionError, queryClient } from '@/lib/api'
import { useListViewState } from '@/lib/detail-navigation'
import { number } from '@/lib/format'
import { browserLanguage } from '@/lib/language-preferences'
import { compareListNames } from '@/lib/list-view'
import {
  alertCardMetric,
  alertCardSummary,
  groupAlertsByCategory,
  notificationChannelTypeLabel,
  notificationChannelTypes,
  ntfyPriorities,
  type NotificationChannel,
  type NotificationChannelSummary,
  type NotificationChannelType,
  type ReleaseAlert,
} from '@/lib/notification-types'
import { useNotification } from '@/lib/notifications'
import { routes } from '@/lib/routes'
import { useSourceNames } from '@/lib/source-names'
import type { Items } from '@/lib/types'
import { CatalogPosterRail } from '@/components/catalog-poster-rail'
import { ConfirmAction, Empty, ErrorNotice, Loading, ResultCount } from '@/components/common'
import { PageActions } from '@/components/page-actions'
import type { PublicInfo } from '@/components/public-connection-fields'
import { ReleaseAlertDialog, type ReleaseAlertTarget } from '@/components/release-alert-dialog'
import { SecretPicker } from '@/components/secret-picker'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

function emptyChannel(): NotificationChannel {
  return {
    id: '',
    name: '',
    type: 'discord',
    enabled: true,
    is_default: false,
    allow_trusted: true,
    config: {},
    secret_ref: '',
    header_secret_ref: '',
    revision: 0,
    created_at: '',
    updated_at: '',
  }
}

function channelSummaryLine(channel: NotificationChannel): string {
  switch (channel.type) {
    case 'discord':
      return [
        channel.config.username ? `Sender: ${channel.config.username}` : 'Discord webhook',
        channel.config.thread_id ? `Thread ${channel.config.thread_id}` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    case 'ntfy':
      return `${channel.config.server_url || 'https://ntfy.sh'}/${channel.config.topic || ''}`
    case 'gotify':
      return channel.config.server_url || 'Gotify server'
    case 'telegram':
      return [
        channel.config.chat_id ? `Chat ${channel.config.chat_id}` : 'Telegram',
        channel.config.thread_id ? `Topic ${channel.config.thread_id}` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    case 'slack':
      return 'Slack / Mattermost incoming webhook'
    case 'webhook':
      return channel.config.header_name
        ? `JSON POST · Header ${channel.config.header_name}`
        : 'JSON POST webhook'
  }
}

export function ActiveAlertsPage({
  trusted = false,
  trustedInfo,
}: {
  trusted?: boolean
  trustedInfo?: PublicInfo
}) {
  const language = browserLanguage()
  const listState = useListViewState()
  const privateSourceName = useSourceNames(!trusted)
  const sourceName = (id: string) =>
    trusted ? trustedInfo?.sources?.find((s) => s.id === id)?.name || id : privateSourceName(id)

  const alertsKey = trusted ? ['trusted-alerts'] : ['alerts']
  const channelsKey = trusted ? ['trusted-notification-channels'] : ['notification-channels']

  const alertsQuery = useQuery({
    queryKey: [...alertsKey, language],
    queryFn: ({ signal }) =>
      api<Items<ReleaseAlert>>(
        `${trusted ? '/trusted/alerts' : '/alerts'}?language=${encodeURIComponent(language)}`,
        { signal },
      ),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })

  const channelsQuery = useQuery({
    queryKey: channelsKey,
    queryFn: ({ signal }) =>
      api<Items<NotificationChannel | NotificationChannelSummary>>(
        trusted ? '/trusted/notifications/channels' : '/notifications/channels',
        { signal },
      ),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })

  const [editing, setEditing] = useState<ReleaseAlertTarget | null>(null)

  const items = alertsQuery.data?.items ?? []
  const sections = groupAlertsByCategory(items)
  const retryingCount = items.filter((item) => item.status === 'retrying').length
  const enabledChannels = (channelsQuery.data?.items ?? []).filter((ch) =>
    'enabled' in ch ? ch.enabled : true,
  )
  const searchPath = trusted ? routes.trusted.search : routes.browse.search

  return (
    <div className="page min-w-0">
      {!trusted && channelsQuery.isSuccess && enabledChannels.length === 0 && (
        <div className="notice flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <p className="text-sm font-medium">No notification channel configured yet</p>
            <p className="help">
              Add a Discord webhook, ntfy topic, Gotify server, Telegram chat, Slack webhook or
              generic webhook in Settings to receive release alerts.
            </p>
          </div>
          <Button asChild size="sm">
            <Link to={routes.connections.alerts.settings}>
              <GearSixIcon aria-hidden="true" />
              Configure channels
            </Link>
          </Button>
        </div>
      )}

      {trusted && channelsQuery.isSuccess && enabledChannels.length === 0 && (
        <div className="notice space-y-1">
          <p className="text-sm font-medium">No notification channels available</p>
          <p className="help">
            The administrator has not enabled any notification channels for trusted users yet.
          </p>
        </div>
      )}

      {trusted && enabledChannels.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>Available channels:</span>
          {enabledChannels.map((ch) => (
            <Badge key={ch.id} variant="outline">
              {ch.name} ({notificationChannelTypeLabel(ch.type)})
            </Badge>
          ))}
        </div>
      )}

      {retryingCount > 0 && (
        <p role="status" className="notice notice-warning">
          {retryingCount === 1
            ? '1 release alert failed to deliver and is retrying automatically. Select its poster to view the error or switch channels.'
            : `${number(retryingCount)} release alerts failed to deliver and are retrying automatically. Select a highlighted poster to view the error or switch channels.`}
        </p>
      )}

      <ErrorNotice error={alertsQuery.error} retry={() => void alertsQuery.refetch()} />

      {alertsQuery.isPending ? (
        <Loading label="Loading release alerts" />
      ) : (
        <>
          <ResultCount total={items.length} label="active alerts" />
          {sections.length === 0 ? (
            <Empty
              icon={BellIcon}
              title="No active release alerts"
              description="Search for a title with no source releases yet (enable “Include titles without releases” in Search filters) and select it to set an alert. Alerts are removed automatically once delivered."
              action={
                !trusted || trustedInfo?.search_enabled ? (
                  <Button asChild variant="outline">
                    <Link to={`${searchPath}?unavailable=true`}>
                      <MagnifyingGlassIcon aria-hidden="true" />
                      Search titles
                    </Link>
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="min-w-0 space-y-8">
              {sections.map((section, index) => (
                <CatalogPosterRail
                  key={section.category.id}
                  category={section.category}
                  items={section.items.map((alert) => {
                    const metric = alertCardMetric(alert, sourceName, !trusted)
                    return {
                      id: alert.work_id,
                      key: alert.id,
                      title: alert.title,
                      cover: alert.cover,
                      summary: alertCardSummary(alert),
                      metric: metric.label,
                      metricTitle: metric.title,
                      to: `${searchPath}/${alert.work_id}`,
                      alertStatus: alert.status,
                      onSelect: () => setEditing({ mode: 'edit', alert }),
                    }
                  })}
                  loading={false}
                  loadingLabel={`Loading ${section.category.label.toLowerCase()} alerts`}
                  countLabel={number(section.items.length)}
                  eagerRow={index === 0}
                  listState={listState}
                  resetScrollKey="alerts"
                />
              ))}
            </div>
          )}
        </>
      )}

      <ReleaseAlertDialog
        open={editing !== null}
        onOpenChange={(next) => {
          if (!next) setEditing(null)
        }}
        target={editing}
        trusted={trusted}
        trustedSources={trustedInfo?.sources}
      />
    </div>
  )
}

export function NotificationChannelsPage() {
  const notify = useNotification()
  const query = useQuery({
    queryKey: ['notification-channels'],
    queryFn: ({ signal }) => api<Items<NotificationChannel>>('/notifications/channels', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const [editing, setEditing] = useState<NotificationChannel | null>(null)
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['notification-channels'] })

  const toggle = useMutation({
    mutationFn: ({ channel, enabled }: { channel: NotificationChannel; enabled: boolean }) =>
      api<NotificationChannel>(`/notifications/channels/${encodeURIComponent(channel.id)}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: channel.name,
          type: channel.type,
          enabled,
          is_default: channel.is_default,
          allow_trusted: channel.allow_trusted,
          config: channel.config,
          secret_ref: channel.secret_ref ?? '',
          header_secret_ref: channel.header_secret_ref ?? '',
          revision: channel.revision,
        }),
      }),
    onSuccess: refresh,
  })

  const testMutation = useMutation({
    mutationFn: (channel: NotificationChannel) =>
      api<{ status: string }>(`/notifications/channels/${encodeURIComponent(channel.id)}/test`, {
        method: 'POST',
      }),
    onSuccess: (_, channel) => {
      notify(`Test notification sent to “${channel.name}”.`)
    },
    onError: (error) => {
      if (!(error instanceof ConnectionError)) {
        notify({ message: error.message, warning: true })
      }
    },
  })

  const items = [...(query.data?.items ?? [])].sort((a, b) => {
    if (a.is_default !== b.is_default) return a.is_default ? -1 : 1
    return compareListNames(a, b)
  })

  return (
    <div className="page">
      <PageActions>
        <Button
          disabled={items.length >= 64}
          onClick={() =>
            setEditing({
              ...emptyChannel(),
              is_default: items.length === 0,
            })
          }
        >
          <PlusIcon aria-hidden="true" />
          Add channel
        </Button>
      </PageActions>

      <ErrorNotice error={toggle.error} scope="action" />
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />

      {query.isPending ? (
        <Loading label="Loading notification channels" />
      ) : (
        <>
          <ResultCount total={items.length} label="notification channels" />
          {items.length === 0 ? (
            <Empty
              icon={BellIcon}
              title="No notification channels"
              description="Configure a Discord webhook, ntfy, Gotify, Telegram, Slack/Mattermost or generic webhook channel to receive release alerts."
            />
          ) : (
            <div className="grid items-stretch gap-4 xl:grid-cols-2">
              {items.map((channel) => (
                <section
                  className="section-card flex flex-col justify-between gap-4"
                  key={channel.id}
                >
                  <div className="space-y-2">
                    <div className="flex min-w-0 items-center justify-between gap-3">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <h2 className="section-title min-w-0 break-words">{channel.name}</h2>
                        <Badge variant="outline">
                          {notificationChannelTypeLabel(channel.type)}
                        </Badge>
                        {channel.is_default && <Badge variant="secondary">Default</Badge>}
                      </div>
                      <Switch
                        aria-label={`Enable ${channel.name}`}
                        checked={channel.enabled}
                        disabled={toggle.isPending}
                        onCheckedChange={(enabled) => toggle.mutate({ channel, enabled })}
                      />
                    </div>
                    <p className="help break-all">{channelSummaryLine(channel)}</p>
                    <p className="text-xs text-muted-foreground">
                      {channel.allow_trusted ? 'Available to trusted users' : 'Administrator only'}
                      {channel.active_alerts
                        ? ` · ${channel.active_alerts} active ${channel.active_alerts === 1 ? 'alert' : 'alerts'}`
                        : ''}
                    </p>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={testMutation.isPending}
                      onClick={() => testMutation.mutate(channel)}
                    >
                      <PaperPlaneTiltIcon aria-hidden="true" />
                      {testMutation.isPending && testMutation.variables?.id === channel.id
                        ? 'Sending…'
                        : 'Send test'}
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setEditing(channel)}>
                      <PencilSimpleIcon aria-hidden="true" />
                      Edit
                    </Button>
                    <ConfirmAction
                      title="Delete notification channel?"
                      description={
                        channel.active_alerts
                          ? `Deleting “${channel.name}” will also permanently remove ${channel.active_alerts} active ${channel.active_alerts === 1 ? 'alert' : 'alerts'} using this channel. Secrets in the vault are kept.`
                          : `Delete “${channel.name}”? Stored secrets in the vault are kept.`
                      }
                      label="Delete channel"
                      variant="destructive"
                      action={async () => {
                        await api<void>(
                          `/notifications/channels/${encodeURIComponent(channel.id)}`,
                          {
                            method: 'DELETE',
                            body: JSON.stringify({ revision: channel.revision }),
                          },
                        )
                        await refresh()
                        await queryClient.invalidateQueries({ queryKey: ['alerts'] })
                        notify('Notification channel deleted.')
                      }}
                      trigger={
                        <Button variant="destructive" size="sm">
                          <TrashIcon aria-hidden="true" />
                          Delete
                        </Button>
                      }
                    />
                  </div>
                </section>
              ))}
            </div>
          )}
        </>
      )}

      {editing && (
        <ChannelEditorDialog
          key={`${editing.id}:${editing.revision}`}
          original={editing}
          close={() => setEditing(null)}
          saved={async () => {
            await refresh()
            await queryClient.invalidateQueries({ queryKey: ['alerts'] })
          }}
        />
      )}
    </div>
  )
}

function ChannelEditorDialog({
  original,
  close,
  saved,
}: {
  original: NotificationChannel
  close: () => void
  saved: () => Promise<unknown>
}) {
  const [value, setValue] = useState<NotificationChannel>(original)
  const notify = useNotification()
  const changed = JSON.stringify(value) !== JSON.stringify(original)
  const dirty = !original.id || changed

  const mutation = useMutation({
    mutationFn: () =>
      api<NotificationChannel>(
        `/notifications/channels${original.id ? '/' + encodeURIComponent(original.id) : ''}`,
        {
          method: original.id ? 'PUT' : 'POST',
          body: JSON.stringify({
            name: value.name,
            type: value.type,
            enabled: value.enabled,
            is_default: value.is_default,
            allow_trusted: value.allow_trusted,
            config: value.config,
            secret_ref: value.secret_ref ?? '',
            header_secret_ref: value.header_secret_ref ?? '',
            revision: value.revision,
          }),
        },
      ),
    onSuccess: async () => {
      await saved()
      notify(original.id ? 'Notification channel updated.' : 'Notification channel created.')
      close()
    },
  })

  const selectedType = notificationChannelTypes.find((t) => t.value === value.type)

  function setType(nextType: NotificationChannelType) {
    setValue({
      ...value,
      type: nextType,
      config:
        nextType === 'ntfy'
          ? { server_url: 'https://ntfy.sh', topic: '', priority: '' }
          : nextType === 'gotify'
            ? { server_url: '', priority: '5' }
            : {},
      secret_ref: '',
      header_secret_ref: '',
    })
    mutation.reset()
  }

  return (
    <Dialog
      open
      unsavedChanges={{
        dirty: changed,
        label: 'Notification channel',
        pending: mutation.isPending,
      }}
      onOpenChange={(open) => {
        if (!open && !mutation.isPending) close()
      }}
    >
      <DialogContent showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <DialogTitle>
              {original.id ? 'Edit notification channel' : 'Add notification channel'}
            </DialogTitle>
            <Switch
              aria-label="Enable channel"
              checked={value.enabled}
              disabled={mutation.isPending}
              onCheckedChange={(enabled) => setValue({ ...value, enabled })}
            />
          </div>
          <DialogDescription>
            Credentials and webhook URLs are stored encrypted in Secrets and never exposed to
            trusted users.
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (!mutation.isPending) mutation.mutate()
          }}
        >
          <fieldset className="min-w-0 space-y-4" disabled={mutation.isPending}>
            <div className="field">
              <Label htmlFor="channel-name">Name</Label>
              <Input
                id="channel-name"
                required
                maxLength={160}
                placeholder="e.g. Discord Movies"
                value={value.name}
                onChange={(event) => setValue({ ...value, name: event.target.value })}
              />
            </div>

            <div className="field">
              <Label htmlFor="channel-type">Platform</Label>
              <select
                id="channel-type"
                className="native-select"
                value={value.type}
                onChange={(event) => setType(event.target.value as NotificationChannelType)}
              >
                {notificationChannelTypes.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              {selectedType && <p className="help">{selectedType.description}</p>}
            </div>

            {value.type === 'discord' && (
              <>
                <SecretPicker
                  label="Discord webhook URL"
                  help="Copy the webhook URL from your Discord channel settings (Integrations → Webhooks)."
                  value={value.secret_ref ?? ''}
                  savedValue={original.secret_ref}
                  prefix="discord-webhook"
                  addLabel="Add webhook URL"
                  valueLabel="Discord webhook URL"
                  required
                  onChange={(secret_ref) => setValue({ ...value, secret_ref })}
                />
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="field">
                    <Label htmlFor="channel-discord-username">Display name (optional)</Label>
                    <Input
                      id="channel-discord-username"
                      maxLength={80}
                      placeholder="AIOSources"
                      value={value.config.username ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, username: event.target.value },
                        })
                      }
                    />
                  </div>
                  <div className="field">
                    <Label
                      htmlFor="channel-discord-thread"
                      help="Optional forum or thread ID to post into a specific thread."
                    >
                      Thread ID (optional)
                    </Label>
                    <Input
                      id="channel-discord-thread"
                      inputMode="numeric"
                      placeholder="Leave empty for channel"
                      value={value.config.thread_id ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, thread_id: event.target.value },
                        })
                      }
                    />
                  </div>
                </div>
              </>
            )}

            {value.type === 'ntfy' && (
              <>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="field">
                    <Label htmlFor="channel-ntfy-url">Server URL</Label>
                    <Input
                      id="channel-ntfy-url"
                      type="url"
                      placeholder="https://ntfy.sh"
                      value={value.config.server_url ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, server_url: event.target.value },
                        })
                      }
                    />
                  </div>
                  <div className="field">
                    <Label htmlFor="channel-ntfy-topic">Topic</Label>
                    <Input
                      id="channel-ntfy-topic"
                      required
                      maxLength={64}
                      placeholder="e.g. aiosources-releases"
                      value={value.config.topic ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, topic: event.target.value },
                        })
                      }
                    />
                  </div>
                </div>
                <div className="field">
                  <Label htmlFor="channel-ntfy-priority">Priority</Label>
                  <select
                    id="channel-ntfy-priority"
                    className="native-select"
                    value={value.config.priority ?? ''}
                    onChange={(event) =>
                      setValue({
                        ...value,
                        config: { ...value.config, priority: event.target.value },
                      })
                    }
                  >
                    {ntfyPriorities.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
                <SecretPicker
                  label="Access token or credentials (optional)"
                  help="Leave empty for a public topic, or store an ntfy access token (tk_…) or username:password."
                  value={value.secret_ref ?? ''}
                  savedValue={original.secret_ref}
                  prefix="ntfy-auth"
                  addLabel="Add token"
                  valueLabel="Token or username:password"
                  onChange={(secret_ref) => setValue({ ...value, secret_ref })}
                />
              </>
            )}

            {value.type === 'gotify' && (
              <>
                <div className="grid gap-3 sm:grid-cols-3">
                  <div className="field sm:col-span-2">
                    <Label htmlFor="channel-gotify-url">Server URL</Label>
                    <Input
                      id="channel-gotify-url"
                      type="url"
                      required
                      placeholder="https://gotify.example.com"
                      value={value.config.server_url ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, server_url: event.target.value },
                        })
                      }
                    />
                  </div>
                  <div className="field">
                    <Label htmlFor="channel-gotify-priority">Priority (0–10)</Label>
                    <Input
                      id="channel-gotify-priority"
                      type="number"
                      min={0}
                      max={10}
                      step={1}
                      placeholder="5"
                      value={value.config.priority ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, priority: event.target.value },
                        })
                      }
                    />
                  </div>
                </div>
                <SecretPicker
                  label="Application token"
                  help="Create an application in Gotify and store its app token in Secrets."
                  value={value.secret_ref ?? ''}
                  savedValue={original.secret_ref}
                  prefix="gotify-token"
                  addLabel="Add app token"
                  valueLabel="Gotify application token"
                  required
                  onChange={(secret_ref) => setValue({ ...value, secret_ref })}
                />
              </>
            )}

            {value.type === 'telegram' && (
              <>
                <SecretPicker
                  label="Bot API token"
                  help="Create a token with @BotFather. No bot server needs to run: AIOSources sends a stateless HTTP POST request."
                  value={value.secret_ref ?? ''}
                  savedValue={original.secret_ref}
                  prefix="telegram-token"
                  addLabel="Add bot token"
                  valueLabel="Telegram Bot API token"
                  required
                  onChange={(secret_ref) => setValue({ ...value, secret_ref })}
                />
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="field">
                    <Label
                      htmlFor="channel-telegram-chat"
                      help="Numeric chat/group ID (e.g. -1001234567890) or public @channel username."
                    >
                      Chat ID
                    </Label>
                    <Input
                      id="channel-telegram-chat"
                      required
                      placeholder="-1001234567890 or @channel"
                      value={value.config.chat_id ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, chat_id: event.target.value },
                        })
                      }
                    />
                  </div>
                  <div className="field">
                    <Label
                      htmlFor="channel-telegram-thread"
                      help="Optional forum topic ID inside a supergroup."
                    >
                      Topic ID (optional)
                    </Label>
                    <Input
                      id="channel-telegram-thread"
                      inputMode="numeric"
                      placeholder="Leave empty for main chat"
                      value={value.config.thread_id ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, thread_id: event.target.value },
                        })
                      }
                    />
                  </div>
                </div>
              </>
            )}

            {value.type === 'slack' && (
              <SecretPicker
                label="Incoming webhook URL"
                help="Works with both Slack and Mattermost incoming webhook URLs."
                value={value.secret_ref ?? ''}
                savedValue={original.secret_ref}
                prefix="slack-webhook"
                addLabel="Add webhook URL"
                valueLabel="Incoming webhook URL"
                required
                onChange={(secret_ref) => setValue({ ...value, secret_ref })}
              />
            )}

            {value.type === 'webhook' && (
              <>
                <SecretPicker
                  label="Webhook URL"
                  help="Receives a structured JSON POST payload containing the title, release details and direct link."
                  value={value.secret_ref ?? ''}
                  savedValue={original.secret_ref}
                  prefix="webhook-url"
                  addLabel="Add webhook URL"
                  valueLabel="Webhook URL"
                  required
                  onChange={(secret_ref) => setValue({ ...value, secret_ref })}
                />
                <Disclosure
                  title={
                    <span className="flex flex-wrap items-center justify-between gap-2">
                      <span>Authentication header (optional)</span>
                      {Boolean(value.config.header_name || value.header_secret_ref) && (
                        <span className="text-xs font-normal text-muted-foreground">
                          Configured
                        </span>
                      )}
                    </span>
                  }
                >
                  <div className="field">
                    <Label htmlFor="channel-webhook-header">Header name</Label>
                    <Input
                      id="channel-webhook-header"
                      maxLength={64}
                      placeholder="e.g. Authorization or X-Webhook-Secret"
                      value={value.config.header_name ?? ''}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          config: { ...value.config, header_name: event.target.value },
                        })
                      }
                    />
                  </div>
                  <SecretPicker
                    label="Header value"
                    value={value.header_secret_ref ?? ''}
                    savedValue={original.header_secret_ref}
                    prefix="webhook-header"
                    addLabel="Add header value"
                    valueLabel="Header value"
                    onChange={(header_secret_ref) => setValue({ ...value, header_secret_ref })}
                  />
                </Disclosure>
              </>
            )}

            <div className="space-y-3 pt-1">
              <div className="flex items-center justify-between gap-3">
                <Label
                  htmlFor="channel-default"
                  help="Pre-selected when creating a new release alert."
                >
                  Default channel
                </Label>
                <Switch
                  id="channel-default"
                  checked={value.is_default}
                  onCheckedChange={(is_default) => setValue({ ...value, is_default })}
                />
              </div>
              <div className="flex items-center justify-between gap-3">
                <Label
                  htmlFor="channel-allow-trusted"
                  help="Allow trusted users with the release alerts permission to send alerts to this channel without seeing its URL or credentials."
                >
                  Allow trusted users
                </Label>
                <Switch
                  id="channel-allow-trusted"
                  checked={value.allow_trusted}
                  onCheckedChange={(allow_trusted) => setValue({ ...value, allow_trusted })}
                />
              </div>
            </div>
          </fieldset>

          <ErrorNotice error={mutation.error} />

          <DialogFooter>
            <Button type="submit" disabled={mutation.isPending || !dirty}>
              <FloppyDiskIcon aria-hidden="true" />
              {mutation.isPending ? 'Saving…' : 'Save channel'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
