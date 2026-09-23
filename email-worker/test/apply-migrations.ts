import migrationSql from "../migrations/0001_sources_and_entries.sql?raw";
import { env } from "cloudflare:test";

// Not `readD1Migrations`/`applyD1Migrations` from the pool package: that
// helper pulls in `miniflare`, which fails to bundle inside the worker
// runtime this setup file itself executes in ("Failed to require
// 'node:process'"). A `?raw` import is a plain Vite static-asset load —
// no Node module resolution at runtime — so it works inside the worker
// sandbox setupFiles run in.
//
// Strip `-- ...` line comments before splitting on `;`: a bare split leaves
// any semicolon inside a comment (there are several in this migration's
// prose) as a false statement boundary, producing "incomplete input" SQLITE
// errors on the fragments either side of it.
const withoutComments = migrationSql.replace(/--.*$/gm, "");
for (const statement of withoutComments.split(";").map((s) => s.trim()).filter(Boolean)) {
	await env.BRIDGE_DB.prepare(statement).run();
}
