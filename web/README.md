# Tianshu Web Frontend

Tianshu SPA frontend built on Vite 8, React 19, TypeScript, TanStack Router/Query/Table, Tailwind CSS v4, shadcn/ui, Zustand, react-hook-form, Zod, and i18next (`zh-CN` default, `en-US`).

- Base UI template derived from `satnaing/shadcn-admin` v2.2.1 (MIT, see [LICENSE](./LICENSE)).
- Frontend architecture & contracts: [../.trellis/spec/frontend/index.md](../.trellis/spec/frontend/index.md).

## Commands

```bash
pnpm dev          # Start Vite dev server (proxies /api to http://localhost:8080)
pnpm typecheck    # Run TypeScript project check
pnpm lint         # Run ESLint (includes layered dependency boundary checks)
pnpm format:check # Verify Prettier formatting
pnpm test         # Run Vitest suite
pnpm build        # Typecheck and build production bundle to web/dist/
```
