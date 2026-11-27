# Landing page

Static HTML/CSS explaining the uploader, leaderboard backups, and automatic
inventory/standings updates. No build step, client-side JavaScript, external
fonts, analytics, package manager, or framework.

## Preview

With Node.js installed, run from the repository root:

```sh
node scripts/serve-site.mjs
```

Open http://127.0.0.1:8080/. Edits are served directly from `site/`; refresh to
see changes. The server maps the shared icon from `assets/icon.svg`, so there is
no copied output directory. Pass a different port as an argument if needed:
`node scripts/serve-site.mjs 8081`. Ctrl+C stops it. Only site files and the shared
icon are served, on the local loopback interface.

## GitHub Pages

One-time repository setup: **Settings → Pages → Build and deployment → Source →
GitHub Actions**. This is not enabled by the workflow itself.

`.github/workflows/pages.yml` publishes `site/` on relevant pushes to `master`,
or via **Actions → Website → Run workflow** on `master`. It copies the shared
icon into the deployment folder; edit `assets/icon.svg`, not a second icon source.
Deployments use the `github-pages` environment and its protection rules. PRs do
not publish. Repository source and application binaries are not included.

The expected project URL is https://echowrought.github.io/uploader/.
Relative asset links also work with a custom domain. Download links go to GitHub
Releases so players choose the appropriate package; the page never downloads or
launches the uploader automatically. Keep the upload/privacy disclosure prominent
and do not promise backup retention or recovery guarantees for the leaderboard.
