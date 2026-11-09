"""Read-only discovery of an existing local performance workspace."""


def default_workspace(repository):
    current = repository / ".aiosources/performance"
    previous = repository / ".ingest/performance"
    # Ownership records contain absolute paths. Keep old workspaces in place
    # rather than moving private data and invalidating their safety checks.
    return previous if previous.exists() and not current.exists() else current
