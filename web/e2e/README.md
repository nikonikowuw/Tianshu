# Playwright E2E

Playwright covers end-to-end user flows against a real Tianshu server and
PostgreSQL database:

- `dashboard.e2e.ts`: Live database dashboard KPI aggregation, time-range
  switching, and `dashboard:read` route guard enforcement.
- `files.e2e.ts`: Files upload, preview, download, and delete lifecycle.
- `settings.e2e.ts`: Personal settings profile/avatar updates, appearance &
  language switching, active session revocation & live 401 redirect, and
  password change.

## Prerequisites

- PostgreSQL reachable with a role that can create schemas and extensions.
- An E2E database (default: `tianshu_e2e`). **Never point this at development or
  production data**: the test server resets `public` before it starts.
- Chromium: `pnpm --dir web test:browser:install`.

The default connection string is:

```text
postgres://tianshu:tianshu@127.0.0.1:5432/tianshu_e2e?sslmode=disable
```

Override it with `TIANSHU_E2E_DSN`. The reset guard refuses non-E2E database
names unless `E2E_ALLOW_RESET=1` is set.

## Run

```bash
cd web
TIANSHU_E2E_DSN='postgres://tianshu:tianshu@127.0.0.1:5432/tianshu_e2e?sslmode=disable' \
  pnpm test:e2e
```

`playwright.config.ts` starts two servers:

- Vite on `127.0.0.1:15173` with `VITE_PROXY_TARGET=http://127.0.0.1:18080`.
- The Go server on `127.0.0.1:18080`, migrations enabled, local storage in a
  temporary directory, and the bootstrap admin from `E2E_ADMIN_*`.

The Vite dev overlay is disabled with `VITE_DISABLE_DEVTOOLS=true`; otherwise the
TanStack devtools button can cover sheet actions in the bottom-right corner.

## Environment

| Variable               | Default                 | Purpose                               |
| ---------------------- | ----------------------- | ------------------------------------- |
| `TIANSHU_E2E_DSN`      | local `tianshu_e2e` DSN | E2E database                          |
| `E2E_API_PORT`         | `18080`                 | Backend port                          |
| `E2E_WEB_PORT`         | `15173`                 | Vite port                             |
| `E2E_ADMIN_USERNAME`   | `e2e-admin`             | Bootstrap admin                       |
| `E2E_ADMIN_EMAIL`      | `e2e-admin@example.com` | Bootstrap admin email                 |
| `E2E_ADMIN_PASSWORD`   | `E2e-Password-123!`     | Bootstrap admin password              |
| `E2E_RESET_DB`         | `1`                     | Drop/recreate `public` before startup |
| `E2E_UPLOAD_DIR`       | temporary directory     | Local storage root                    |
| `E2E_LOGIN_RATE_LIMIT` | `30`                    | Per-IP/min login limit for E2E runs   |

Failures keep traces, screenshots, and video under `web/test-results/`.
