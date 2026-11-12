# Trusted users

Trusted users use your instance’s debrid accounts and torrent sources without accessing administration. Credential sources are chosen independently for Public and Trusted. Creating a trusted account does not enable public access.

## Create an account

1. Open **Administration → Users → Trusted** and choose **Add user**.
2. Enter the person’s email and a password of at least 12 characters. No email is sent.
3. Under **Permissions**, choose Search, Torznab, Stremio or Shared catalogs as needed. All four are preselected for a new account, which stays disabled until you enable it.
4. In **Credentials**, choose instance or personal debrid accounts and announce URLs independently. Both instance options start enabled for a new trusted account.
5. Enable the account, save, and give its credentials to the person privately.

Passwords are hashed with Argon2id. Passwords and instance debrid API keys are never included in connection links or account lists. Personal keys and announces, when required, are encrypted inside the user’s configuration.

When **Use instance announce URLs** is enabled, Torznab clients receive your private-source announce URLs in magnets. Authorize only recipients you trust with these URLs. When disabled, they can supply personal announces; without one, private sources remain cached-only. Stremio debrid playback and browser downloads keep debrid API keys on the server. Requests use the selected accounts and remain subject to their limits.

## Sign in and connect

Choose **Login** on the visitor homepage. The **Trusted user** tab is selected by default; **Administrator** is a separate tab. Use the email and password supplied by the administrator.

Only permitted pages appear. With instance debrid accounts enabled, no personal service key is requested. Otherwise, enter personal keys in the same forms as public visitors. Keys and optional personal announces are remembered in an encrypted browser configuration scoped to that account, never in the server database. Torznab and Stremio reuse the usual filters and sorting controls. Create separate links for different choices. Shared catalogs use the same read-only data permissions configured under **Shared catalog data**; they never export credentials or announce URLs.

Connection links grant access without the browser session, so keep them private. Signing out closes that browser session but leaves links installed in external clients working.

## Change or remove access

Edit an account to change its permissions or replace its password. Saving an account change revokes its existing sessions and connection links. The person signs in again and creates new links. Re-enabling an account does not restore revoked links.

Delete an account to remove it and revoke its access. This leaves source records, shared catalog data and instance credentials intact. There is no password reset email; an administrator sets a new password here when needed.
