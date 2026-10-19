# Troubleshooting

obramat.es is behind DataDome and the CLI replays a browser session, so most problems are "the site did
not trust the session" or "the address is temporarily blocked". Two commands answer almost every one of
them.

## Start here

```bash
obramat doctor --format pretty
```

`doctor` checks, without changing anything: the config parses, the sinks are valid, one probe search
answers **and parses**, the default store resolves, the watch database opens, and the timer is installed.
A `FAIL` line names the piece and the reason; when the site is blocked it is the `search` line that
fails.

```bash
obramat auth status --check
```

`auth status` prints where the session comes from (file, `OBRAMAT_COOKIEJAR`, or none), the cookie
names it holds, whether a `datadome` cookie is present, the imported user agent, and then — with
`--check`, one request — either `storefront  works` or `refused: <why>`. Cookie values are never
printed. No session at all makes `--check` exit 3 with the command to run.

## "bot protection refused the request" (HTTP 403)

```
obramat: obramat.es bot protection refused the request (HTTP 403). Wait a few minutes before retrying, and keep the request rate low
  try: wait a few minutes, then retry once
```

That is exit 5: DataDome did not trust the request. Re-import a fresh session and look again:

```bash
obramat auth harvest          # borrows your real browser's session automatically
obramat auth login --cookies ~/Downloads/obramat-cookies.json
obramat doctor
```

After a block the CLI also stays away from the site for 10 minutes on purpose, because retrying at once
keeps the block in place:

```
obramat: paused until 14:32 after obramat.es refused a request; trying sooner tends to extend the block (delete .../obramat-cli/blocked-until to override)
```

Wait it out rather than deleting the file, and do not run a loop of commands.

## My browser loads the site, but the CLI is challenged

The CLI does not use your browser: it replays a **stored** copy of the session (cookies and user agent
from when you last imported). Browsers refresh the DataDome cookie constantly, so a stored session goes
stale while the browser keeps working. This is the most common case.

```bash
obramat auth harvest   # copy the current, trusted browser session in
obramat doctor
```

`auth harvest` reads the session out of your real Chrome profile, so the two stay in step. Run it again
whenever the site starts challenging the CLI.

## `auth harvest` opens Chrome and is still blocked

A brand-new Chrome profile has no trusted `datadome` cookie, so DataDome challenges it immediately.
`auth harvest` handles this: it copies **your real Chrome profile's** cookie store into a temp profile
and starts Chrome on that copy, so the session it reads is your trusted one. The copy is deleted when
the command ends.

If it is still blocked:

- give the window a moment to load the storefront before the timeout (the default is 5 minutes,
  `--timeout` changes it);
- if the window really shows a captcha, clear it by hand — the command waits;
- if the CLI says it copied a profile but the browser never opened, the profile may be in use by a
  running Chrome, or the keyring is not available to decrypt the copy; `obramat auth login --cookies`
  with an export still works;
- otherwise the block is about the address — see the next item.

## "Access temporarily restricted", with an incident ID

That page (or a 403 that persists for a long time) is an **IP-reputation** block, not a session problem.
Re-importing will not help. It is temporary — usually one to four hours, occasionally longer. To get out
of it sooner:

- stop hitting obramat.es from this network, including your browser and any watches;
- keep the CLI's rate low (`watch` intervals, one command at a time) once it clears;
- then try a single `obramat doctor`.

## How do I export cookies?

Any of these, then `obramat auth login --cookies FILE` (or `--har FILE`, or pipe it with
`--cookies-stdin`; `--cookies-format` forces `auto`, `json`, `netscape`, `header` or `har`):

- **Cookie-Editor / EditThisCookie** JSON — the array, or the `{"url","cookies"}` wrapper;
- **Netscape** `cookies.txt`;
- a raw **`Cookie:` header** copied from DevTools;
- a **HAR** saved from DevTools → Network with *Allow to generate HAR with sensitive data*.

Only `obramat.es` cookies are kept, so a whole-browser export is safe to pass. An encrypted Cookie-Editor
export needs its password before it becomes readable JSON — the CLI cannot open the encrypted file itself,
so export the plain format.

For scripts and CI, `OBRAMAT_COOKIEJAR=FILE` reads an export on every run without storing it; in that
mode `auth login` refuses to shadow it.

## Stock only shows home delivery, never store pickup

A store is not selected, so the page shows the site's default web price and no per-store stock. Pick
your warehouse in the site's store picker, or set it for the CLI:

```bash
obramat store list --format pretty
obramat config set store 4          # a store code; also --store, or OBRAMAT_STORE
obramat auth harvest                # re-harvest so the session carries the chosen store
```

Without a store the site may also show a Canary Islands (IGIC) price, which is not the price you would
pay on the mainland.

## Will this get me banned? Is it allowed?

It is a personal, read-only tool. It uses no account — only the guest session your own browser already
has — and asks the site for what one browser page view asks for. It is not a crawler: requests are spaced
(400 ms, 1.5 s inside sweeps), watches are floored at 5 minutes, and there is no bulk mode.

For the record, `robots.txt` disallows some of the paths the CLI uses (`/search?q=*`, the filter and sort
parameters, `/lazy/` and `/rest/`); this is documented in
[obramat-api-discovery.md](obramat-api-discovery.md). Keep the rate low, do not scrape in bulk, and treat
it as one person browsing.

Exit codes and the JSON error envelope are in [cli-spec.md](cli-spec.md#7-exit-codes); run any command
with `--debug` to log every request, or `--error-format json` to script on the failure.
