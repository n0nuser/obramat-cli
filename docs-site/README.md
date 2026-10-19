# docs-site

The obramat-cli documentation: Fumadocs on Next.js, exported as static files and published to GitHub
Pages by `.github/workflows/pages.yml`. The docs domain is not set yet (`DOCS_DOMAIN` in the README is a
placeholder).

```bash
npm ci
npm run dev        # http://localhost:3000/docs
npm run typecheck
npm run build      # static export in out/
npm start          # serve out/ (needs Python 3)
```

`content/docs/reference/*.mdx` is written by `cmd/docsgen` from the CLI's command tree. Edit the
command's help text in Go and run `make docs` from the repository root; `make docs-check` (CI) fails
when the pages are stale. `content/docs/reference/index.mdx` is hand-written and left alone.

Search runs in the browser on the index built at `/api/search`; `/llms.txt` is generated too.
