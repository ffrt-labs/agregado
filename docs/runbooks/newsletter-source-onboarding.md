# Onboarding a newsletter Source

Applies to the Resend + n8n Bridge ([ADR-0007](../adr/0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md)).
There is no `FORWARD_EMAIL` and no Gmail forwarding; confirmation mail is read in
Resend's dashboard.

1. **Create the Source row** in D1 with `status = 'pending'`. The alias is the
   local-part of the address; the feed secret is stored only as its SHA-256:

   ```sh
   SECRET=$(openssl rand -hex 24); HASH=$(printf %s "$SECRET" | sha256sum | cut -d' ' -f1)
   npx wrangler d1 execute bridge --remote --command \
     "INSERT INTO sources (id, display_name, basic_auth_secret_hash, status) VALUES ('tldr', 'TLDR', '$HASH', 'pending')"
   ```

   Keep `$SECRET`; it is the Basic-auth password Miniflux will use.
2. **Subscribe** at the publisher using `tldr@<your-domain>` (catch-all; no Resend-side setup).
3. **Confirmation mail** arrives at Resend. n8n no-ops on it because the Source is `pending`.
4. **Read it in Resend's dashboard** (Emails → Receiving), click the confirmation link,
   then activate the Source:

   ```sh
   npx wrangler d1 execute bridge --remote --command "UPDATE sources SET status = 'active' WHERE id = 'tldr'"
   ```

   Activation is manual on purpose: confirmation mails vary too much to auto-detect.
5. **Subscribe in Miniflux** to `https://<bridge-host>/feed/tldr.atom` with Basic auth
   (any username, `$SECRET` as password).

Mail to an unregistered alias is dropped without alert. A `pending` Source's mail is
dropped too, so re-request the confirmation if it arrived before step 1.
