#!/usr/bin/env bash
# Creates every index this service depends on, without starting the server.
#
# The application already does this itself on every boot — see
# internal/db/client.go's EnsureIndexes, called from internal/app/app.go —
# and index creation in MongoDB is idempotent, so running this script changes
# nothing a normal boot wouldn't already do. Its purpose is deployment
# timing: building a text index and a handful of others on a large existing
# collection can take real time, and you may prefer to pay that cost once,
# ahead of a deploy, rather than having the very first boot of a new version
# block on it while live traffic is arriving. Safe to run repeatedly, and
# safe to skip entirely — the application will still create anything missing
# the moment it starts.
#
# Requires mongosh. Reads MONGO_URI and MONGO_DB from the environment, with
# the same defaults internal/config/config.go uses.
set -euo pipefail

MONGO_URI="${MONGO_URI:-mongodb://localhost:27017}"
MONGO_DB="${MONGO_DB:-evernote_lite}"

if ! command -v mongosh >/dev/null 2>&1; then
	echo "error: mongosh is not installed or not on PATH" >&2
	exit 1
fi

echo "Creating indexes on ${MONGO_DB} at ${MONGO_URI} ..."

mongosh "${MONGO_URI}/${MONGO_DB}" --quiet <<'JS'
// users — see internal/db/users_repo.go ensureUserIndexes
db.users.createIndex({ email: 1 }, { unique: true, name: "uniq_email" });

// revoked_tokens — see internal/db/revoked_repo.go ensureRevokedTokenIndexes
db.revoked_tokens.createIndex({ jti: 1 }, { unique: true, name: "uniq_jti" });
db.revoked_tokens.createIndex(
	{ expires_at: 1 },
	{ expireAfterSeconds: 0, name: "ttl_expires_at" }
);

// notes — see internal/db/notes_repo.go ensureNoteIndexes
db.notes.createIndex({ owner_id: 1, updated_at: -1 }, { name: "owner_updated" });
db.notes.createIndex(
	{ title: "text", body: "text" },
	{ name: "text_title_body" }
);
db.notes.createIndex({ tags: 1 }, { name: "tags_multikey" });

// shares — see internal/db/shares_repo.go ensureShareIndexes
db.shares.createIndex({ token: 1 }, { unique: true, name: "uniq_token" });
db.shares.createIndex({ note_id: 1 }, { name: "note_id_idx" });

print("Indexes created.");
JS

echo "Done."
