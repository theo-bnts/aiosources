export function FormStatus({
  dirty,
  saving,
  saved,
  message,
  savedMessage = 'Changes saved.',
}: {
  dirty: boolean
  saving: boolean
  saved?: boolean
  message?: string
  savedMessage?: string
}) {
  return (
    <p
      role="status"
      className={`help ${saved && !dirty && !saving && !message ? 'text-primary' : ''}`}
    >
      {saving
        ? 'Saving changes…'
        : message || (dirty ? 'Unsaved changes' : saved ? savedMessage : 'No unsaved changes')}
    </p>
  )
}
