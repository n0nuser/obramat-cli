# 0002. Read-only, no account

Date: 2026-10-19
Status: accepted

## Context

The point of the CLI is deciding where to buy building materials: what a product costs at each
warehouse and whether it is there. Everything needed for that (catalogue, per-store prices, per-store
stock, store details) is on public pages. The site also has a cart, quotes and orders behind an account
login.

## Decision

No cart, no orders, no quotes, no login. Every command is a read. The MCP server exposes only reads.

## Consequences

- No credentials to store, protect or expire, so no `auth` noun and no `--profile`. Exit code 3 stays
  reserved for symmetry with the error table and is never produced.
- Buying stays in the browser. `product show` prints the product URL to hand off to.
- If an account-only read (for example professional prices) becomes worth having, it needs its own ADR
  for credential handling.
