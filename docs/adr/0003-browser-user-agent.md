# 0003. A browser User-Agent, plain net/http

Date: 2026-10-19
Status: accepted

## Context

obramat.es sits behind DataDome. A request with an honest User-Agent (`obramat-cli/0.1.0`) is refused
with a 403 and a captcha page; the same request with a browser User-Agent and ordinary `Accept` /
`Accept-Language` headers is answered. Go's standard TLS client is accepted: no TLS-fingerprint
impersonation was needed, which keeps the binary free of uTLS-style dependencies and cgo.

## Decision

Send a browser User-Agent with `obramat-cli/<version>` appended, `Accept` and `Accept-Language: es-ES`.
No other evasion: no cookie reuse, no fingerprint spoofing, no header shuffling. Space requests at least
400 ms apart within a process; floor watch intervals at 5 minutes. A 403 recognised as DataDome, or any
429, is exit 5 with advice to wait, and `watch check` stops at the first one.

## Consequences

- If the site raises its protection to something only a real browser passes, the CLI fails with exit 5
  rather than escalating. Going further (TLS impersonation, a headless browser) is a separate decision.
- The string `captcha-delivery.com` appears in every normal page's Content-Security-Policy, so block
  detection keys on the status code first (`TestNormalPageIsNotABlock`).
