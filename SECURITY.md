# Security Policy

## Supported versions

Reverb is under active development. Security fixes are provided for the **latest
released version** only. Please upgrade to the latest release before reporting an
issue.

| Version | Supported |
| --- | --- |
| Latest release | ✅ |
| Older releases | ❌ |

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately through one of:

- **GitHub Security Advisories** (preferred) — open a private advisory at
  <https://github.com/maxjb-xyz/reverb/security/advisories/new>.
- **Email** — security@reverb.example _(placeholder — replace with the real
  contact before public release)_.

Please include:

- A description of the vulnerability and its impact.
- Steps to reproduce (proof-of-concept if possible).
- The Reverb version (from the footer or `GET /api/v1/version`) and your
  deployment method (Docker image or built from source).

## What to expect

- We'll acknowledge your report as soon as we can.
- We'll investigate, keep you updated on progress, and coordinate a fix and
  disclosure timeline with you.
- Please give us a reasonable window to release a fix before any public
  disclosure.

## Note on data at rest

Adapter credentials and the bundled-Navidrome admin password are currently stored
**unencrypted** in Reverb's SQLite database. Treat the data volume as sensitive.
See [docs/deployment.md](docs/deployment.md#secrets-at-rest).
