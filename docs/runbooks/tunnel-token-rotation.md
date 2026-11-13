# Runbook — Cloudflare Tunnel token rotation

Issue #167. The `cloudflared` service connects the public ingest tunnel (the
n8n bridge traffic, ADR-0008) using `TUNNEL_TOKEN`.

## Single source of truth

The **`CLOUDFLARE_TUNNEL_TOKEN` GitHub Actions secret** is the only source of
the token for production. `.github/workflows/deploy.yml` exports it, and
Compose resolves shell variables ahead of `.env`, so **a `CLOUDFLARE_TUNNEL_TOKEN`
in the host's `.env` is ignored by production deploys**. Do not keep a second
copy there.

## Deploy gate

The `cloudflared` service has a healthcheck that passes only once the tunnel
has a registered connector (`cloudflared tunnel ready`, backed by `/ready`).
The "Start service" step runs `up --wait --wait-timeout 120`, so a deploy with
a stale or invalid token fails the run (red check) after at most 120s. The log
names the token as the likely cause and prints the `cloudflared` logs
(`Tunnel not found` / `Invalid tunnel secret`). Container logs stay on the host:
`docker compose --profile prod logs cloudflared`.

## Rotate the token

1. Cloudflare Zero Trust dashboard → Networks → Tunnels → select the tunnel →
   Configure → copy the token (or refresh it, if rotating a leaked one).
2. GitHub → ffrt-labs/agregado → Settings → Secrets and variables → Actions →
   update `CLOUDFLARE_TUNNEL_TOKEN`.
3. Re-run the latest "Deploy to local server" workflow run (Actions → Re-run
   all jobs), or push to `main`.
4. Confirm: the run is green, and on the host
   `docker compose --profile prod ps cloudflared` shows `healthy`. The tunnel
   shows **Healthy** in the Cloudflare dashboard, and Resend webhook deliveries
   stop returning error 1033.

If the run is red at "Start service", the secret is wrong or stale: repeat
steps 1–2, then re-run.
