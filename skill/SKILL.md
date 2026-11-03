---
name: tinyhost
description: Publish static websites, reports and JSON data to this tinyhost server; create temporary previews and obtain owner-approved Agent access. Use when a user needs a shareable web result from files in your workspace.
---

# tinyhost

Server: {{SERVER}} · CLI/server release: {{VERSION}}

tinyhost hosts static HTML, CSS, JavaScript, images and JSON. It does not run backend processes. Use `preview` while iterating, `deploy` for a permanent App with immutable versions, and `data push` to update a declared data directory without replacing the App code.

## Install and authorize

```sh
curl -fsSL --retry 3 --retry-all-errors {{SERVER}}/install.sh | sh
tinyhost skill
tinyhost login --name "my-agent" --wait 60
```

The retry options ride out brief CDN errors (52x) and dropped connections; the installer retries its own downloads. `--retry-all-errors` needs curl 7.71 or newer: if curl reports it as unknown, drop it and run the line again. Installation writes the server to the platform config directory. If `~/.local/bin` is outside PATH, follow the installer's PATH instruction. Re-run the installer to upgrade. Server precedence: `--server URL` > `TINYHOST_SERVER` > config.json. Use `--server {{SERVER}}` to select this server explicitly.

Send the **exact approval link and verification code** printed by login to the user. Wait for the user to approve on the admin domain. Owner passwords belong only on that admin domain. Never send device codes, tokens, credentials.json or pending-login.json to the user or commit them. Exit 4 means approval is pending: run `tinyhost login` again to continue the same request; `--wait 120` extends the wait. Expired or rejected requests are cleared; retry login to create a new one. `--force` requests a fresh authorization even if already signed in.

In cloud environments, a securely supplied `TINYHOST_TOKEN` overrides saved credentials. Login with that variable uses it directly and does not create a request. `tinyhost whoami` shows the Agent, server and expiry. `tinyhost logout` revokes the current token and clears its saved credential; also unset TINYHOST_TOKEN if it was used. Credentials are stored separately per server with mode 0600.

## Prepare an App

Use a directory containing `index.html`, or pass one HTML file (uploaded as index.html). Keep assets relative: `./app.js`, `./assets/site.css`, `data/sales.json`. The same files appear under `/slug/`, `/slug@vN/` and `/_p/token/`; root-relative URLs such as `/assets/app.js` break under those prefixes. Use hash routing for SPAs. JSON files are a convenient data source.

Before upload, deploy, data push and preview warn about root-relative references with file, line and source text. Fix them to relative paths; warnings do not block uploads. Scanning is bounded (text files up to 1 MiB, five findings per file, twenty overall), so a clean scan is not a guarantee that every runtime URL works. Dotfiles, node_modules and symlinks are excluded except .well-known. Maximum upload: 50 MiB and 1000 files by default; the server may set different limits.

Optional `tinyhost.json` in the App root:

```json
{
  "app": "sales-report",
  "title": "Sales report",
  "data": {"data/": "sales.json: array of {month, revenue}"}
}
```

This file is metadata, not a public web asset. `app` supplies the default slug. After successful deploy the CLI remembers the slug in the local manifest and preserves unknown fields. `--title` overrides the manifest title; omitting both preserves an existing title. The `data` map documents directory contracts for other Agents.

## Preview while iterating

```sh
tinyhost preview ./site
tinyhost preview ./site --ttl 1h
tinyhost preview ./site --new
tinyhost previews
tinyhost previews rm 'PREVIEW_URL_OR_ID'
```

The same Agent, server and absolute local path reuse the same URL and refresh its expiry. TTL defaults to 24h and must be between 1h and 168h (7 days). `--new` creates a separate link; subsequent previews of that path update the new link. The old link survives until expiry or deletion. Preview links are public to anyone who has them, even without login. They expire with HTTP 410 and become 404 after cleanup seven days later. Previews are separate from Apps, create no versions and have no data overlays. Agents can list and delete only their own previews.

## Publish and inspect

Check `tinyhost apps` and `tinyhost info SLUG` before touching an existing App. Do not publish over another Agent's App unless the user requested that collaboration. New App slugs are 3–40 lowercase letters/digits/hyphens, alphanumeric at both ends, no consecutive hyphens; system names are reserved.

```sh
tinyhost deploy ./site --app sales-report --create --title "Sales report"
tinyhost deploy ./public-site --app public-report --create --public
tinyhost deploy ./site --app sales-report -m "Updated chart"
tinyhost deploy ./site --app sales-report --no-promote
tinyhost apps
tinyhost info sales-report
tinyhost versions sales-report
tinyhost promote sales-report v2
```

Creating an App requires `--create`; omit it on later deploys. New Apps default to private (Owner login required to view). `--public` applies only with `--create`. Agents cannot change visibility or rename Apps; ask the Owner to use the admin console. Never put keys, tokens or private information in a public App or a preview.

Each successful publish creates an immutable version. Default publishing moves the main URL to it; `--no-promote` leaves the main URL unchanged. Rollback with `promote` changes only that pointer. Future changes build on the version currently selected by the main URL, including after rollback. **After publishing, give the user both the exact version URL and the exact main URL from the CLI output.** For previews, send the exact preview URL and expiry.

## Update data independently

First run `tinyhost info sales-report` and read its declared data directories and format. Prepare the full replacement for one directory, matching that contract:

```sh
tinyhost data push ./data --app sales-report --to data/ -m "November data"
tinyhost data rm --app sales-report --to data/
```

An overlay replaces its entire directory; missing files do not fall back to the bundled directory. Publishing the App bundle retains current overlays. Removing an overlay creates a new version that reveals the App bundle's files again. Overlapping overlay directories are rejected. Any approved Agent can publish or update data, but may delete only Apps it created:

```sh
tinyhost delete sales-report
```

Deletion moves the App to trash. The Owner can restore it within seven days (default) or purge it early. Rename, visibility changes and individual version deletion are Owner console operations.

## Machine output and curl

Global `--json` works before or after commands. Success and error JSON go to stdout; warnings and upgrade hints go to stderr. Optional `warnings` on upload results contain `file`, `line`, `text`. Exit codes: 0 success, 1 general failure, 2 usage, 3 unauthorized, 4 approval pending. Useful local commands: `tinyhost help`, `tinyhost version`, `tinyhost skill` (all also accept --json).

Without the CLI, create a safe tar.gz with index.html at its root and upload directly. curl uploads do not run path checks:

```sh
tar -czf site.tar.gz -C ./site .
curl -fsS -H "Authorization: Bearer $TINYHOST_TOKEN" \
  -H 'Content-Type: application/gzip' --data-binary @site.tar.gz \
  '{{SERVER}}/_/api/v1/apps/sales-report/deploys?create=1'
```

Omit `?create=1` for an existing App; use `visibility=public` only when deliberately creating a public App. Agent APIs accept Bearer tokens, never browser cookies. Do not add retry options to uploads: after a 52x or dropped connection, check the App's versions before uploading again. The CLI retries only read-only requests.

## Common errors

- `unauthorized` / exit 3: run login again, or replace a stale TINYHOST_TOKEN through its secure source.
- `app_not_found`: verify the slug with apps; use --create only if creating a new App was intended.
- `slug_taken`: inspect the existing App and choose a new slug if unrelated; do not overwrite it merely to resolve the error.
- `overlay_conflict`: inspect info and select a non-overlapping directory; agree on data contracts before replacing data.
- `bad_bundle`: ensure index.html exists, remove unsafe paths/symlinks and reduce file count or size.
- Preview TTL error: use Go durations such as 1h, 24h, 168h (not 30d).
- Preview 410/404: run preview again and share the returned link.
- Root-relative path warnings or broken assets: fix URLs to be relative, rebuild and republish.
- Network errors: confirm the configured server and connectivity before retrying. If the CLI reports upload success plus a local manifest warning, publishing already succeeded; repair the local file instead of publishing a duplicate version.
