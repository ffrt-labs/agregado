# Onboarding a newsletter Source

Applies to the Resend + n8n Bridge ([ADR-0007](../adr/0007-resend-n8n-cloudflare-split-for-newsletter-ingestion.md)).
There is no `FORWARD_EMAIL` and no Gmail forwarding; confirmation mail is read in
Resend's dashboard.

## With the Newsletters app

From a tailnet device, open `https://newsletters.felipefreitas.dev`
(`cmd/bridge-admin`, [ADR-0012](../adr/0012-newsletter-sources-are-managed-by-a-tailnet-only-homelab-app.md)).
**Add a newsletter**: enter the display name, check the suggested alias and the
Miniflux category, and save. The app creates the Pending Source in D1 and
subscribes Miniflux to its feed with a fresh password, which it does not keep.
If Miniflux refuses the subscription, nothing is saved; fix the cause and add
it again.

Then subscribe at the publisher with the receiving address the List shows.
Open the Source's **Confirm** link (Pending Sources only). It lists the mail
Resend received for that address with the links found in each email; follow
the confirmation link, then press **Activate**. The emails themselves are
never shown.

The List's **Unclaimed mail** section shows recent mail to aliases with no
Source. **Register this alias** opens Add with the alias filled in.

The app reads mail with its own Resend key, `BRIDGE_ADMIN_RESEND_API_KEY`.
Resend has no read-only key type and a sending-only key cannot read received
mail, so create a full-access key for the app (Resend dashboard, API Keys),
separate from n8n's.

Click a Source's name to **Rename** it (the display name in D1, which is the
feed title, and the Miniflux feed title change together; the alias never
changes) or **Reconnect** it. Reconnect rotates the feed password and gives the
new one to Miniflux, subscribing the feed again if it was deleted there. Use it
when the List shows "Fetch failing" with a 401 or "No Miniflux feed". If it
reports a Miniflux error after saving the new hash, Reconnect again.

## By hand

The manual flow, for when the app is down.

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
5. **Subscribe in Miniflux** to `https://email-worker.f11s.workers.dev/feed/tldr.atom`
   with Basic auth (any username, `$SECRET` as password). That is the Bridge's
   host until `bridge.felipefreitas.dev` gets its DNS record
   ([ADR-0008](../adr/0008-agregado-is-tailnet-only.md)); see
   [n8n/README.md](../../n8n/README.md#configuration-n8n)
   for why the article-enrichment workflow's `BRIDGE_ORIGIN` must match it.

Mail to an unregistered alias is dropped without alert. A `pending` Source's mail is
dropped too, so re-request the confirmation if it arrived before step 1.

## Find an entry's id

An entry's id is the SHA-256 of the Message-ID **as received from Resend**, with
the `<>` stripped. Resend rewrites the Message-ID on inbound
(`...@eu-west-1.amazonses.com`), so you cannot derive the id from the mail as
sent. Two ways to get it:

- **From the delivered Message-ID.** Open the n8n execution for the mail and
  copy the `Message-ID` header from the output of the `Fetch email` node (the
  webhook body itself carries no headers), then:

  ```sh
  printf %s 'id@host' | sha256sum | cut -d' ' -f1
  ```

  Pass the value without the angle brackets.
- **From D1.** Look the entry up by Source and title:

  ```sh
  npx wrangler d1 execute bridge --remote --command \
    "SELECT id, title FROM entries WHERE source_id = 'tldr' AND title LIKE '%keyword%'"
  ```

  The same id is the entry's R2 object key and its Atom `<id>`.
