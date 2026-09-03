# Evernote-Lite

A backend REST API for personal note-taking, written in Go. Users sign up, log
in with a JWT, and manage their own notes — creating, editing, tagging,
searching and soft-deleting them. Any note can be turned into a public,
read-only share link that works without an account.

Backend only: there is no frontend. The deliverable is the API, its data model,
and the operational scaffolding around it.

**Status: feature-complete.** Every feature below is implemented and tested.
The one open item is under [Testing](#testing): nothing has yet been run
against a real MongoDB instance.

- [Features](#features)
- [Stack](#stack)
- [Project layout](#project-layout)
- [Running it](#running-it)
- [API](#api)
- [Security notes](#security-notes)
- [Testing](#testing)
- [Deployment](#deployment)

## Features

- **Authentication** — signup, login, and real logout: each JWT carries a
  unique `jti`, and logging out records it in a denylist the auth middleware
  checks on every request, so a logged-out token stops working immediately
  rather than just expiring on its own schedule. Passwords are hashed with
  bcrypt; tokens are signed HS256.
- **Notes CRUD** — create, read, update (every field optional, so a partial
  update can't silently reset a field it didn't mention), and soft delete.
  Ownership is enforced through one shared code path on every request, not
  re-checked ad hoc per handler.
- **Listing, search and tags** — full-text search across title and body, tag
  filtering, pagination with an accurate total, sorting against an allowlist,
  and a tag-aggregation endpoint.
- **Public sharing** — a note can be turned into a read-only link that works
  with no account at all, with an optional password (checked via a header,
  never a query parameter) and an optional expiry. Click counting and
  soft-delete-awareness are both handled correctly by construction, not as
  special cases.
- **Middleware** — structured request logging, CORS, rate limiting on the
  endpoints that need it, and panic recovery that returns the same error
  envelope as every other failure rather than a bare 500.
- **Tests and deployment** — unit tests and handler-level tests run through
  the real router against fake stores, a two-stage Dockerfile, a
  docker-compose stack for local development, a Makefile, and AWS ECS
  deployment artifacts.

## Stack

| Piece | Used for |
|---|---|
| Go 1.25 | Language and toolchain |
| Gin | HTTP router and middleware chain |
| MongoDB (driver v2) | Document store |
| JWT (HS256) | Stateless bearer authentication |
| bcrypt | Password hashing, cost 12 |

## Project layout

```
cmd/evernote-lite/     application entry point
internal/
  app/                 wire-up: router, middleware, database init
  config/              configuration structs and environment loaders
  handlers/            HTTP handlers grouped by feature
  models/              MongoDB document types
  db/                  data access layer — every Mongo query lives here
  middleware/          auth, logging, CORS, rate limiting
  utils/               leaf helpers: hashing, tokens, strings
  schemas/             request and response DTOs
scripts/               setup helpers (index initialisation)
configs/               sample configuration
docs/                  API documentation
tests/                 unit and integration tests
deploy/                deployment manifests
```

Requests flow in one direction only: middleware, then handlers, then the data
access layer, then the models. Handlers never issue a database query
themselves, which keeps the access-control rules independent of any query and
testable without a database running.

## Running it

Requires Go 1.25+ and a reachable MongoDB.

```bash
cp .env.example .env
# generate a signing key and paste it into JWT_SECRET
openssl rand -hex 32

go run ./cmd/evernote-lite
```

The service refuses to start without `JWT_SECRET`, with a `BCRYPT_COST` below
10, or when MongoDB is unreachable — a misconfiguration fails at boot rather
than on the first request. Required indexes are created on startup.

### Running it with Docker

```bash
make docker-up      # API + MongoDB together, for local development
make docker-down     # stop and remove them
```

This runs [deploy/docker-compose.yml](deploy/docker-compose.yml), which builds
the image from the root [Dockerfile](Dockerfile) and starts it against a
throwaway local MongoDB container — no `.env` needed for this path, since the
compose file supplies development defaults itself (see the file for why
`JWT_SECRET` there is a fixed, clearly-labelled placeholder rather than a
generated one). The API waits for MongoDB's own health check to pass before it
starts, rather than for the container to merely exist.

### Makefile targets

```bash
make build        # compile ./bin/evernote-lite
make run           # go run the server locally
make test          # go test ./...
make test-race     # go test -race ./...
make lint          # gofmt -l + go vet
make tidy          # go mod tidy
make docker-build  # build the container image only
```

### Configuration

All settings come from the environment; nothing is hardcoded and no secret is
committed.

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `MONGO_URI` | `mongodb://localhost:27017` | |
| `MONGO_DB` | `evernote_lite` | |
| `JWT_SECRET` | — | Required, no default |
| `JWT_EXPIRY` | `1h` | Any Go duration |
| `BCRYPT_COST` | `12` | Minimum 10 |
| `APP_BASE_URL` | `http://localhost:8080` | Used to build share URLs |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated, or `*` for any origin |
| `RATE_LIMIT_REQUESTS` | `20` | Per caller, per window, on auth + `/s/:token` |
| `RATE_LIMIT_WINDOW` | `1m` | Any Go duration |

## API

Base path `/api/v1`. Protected endpoints require `Authorization: Bearer <token>`.

### Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/healthz` | — | Liveness, including a MongoDB ping |
| `POST` | `/api/v1/auth/signup` | — | Create an account |
| `POST` | `/api/v1/auth/login` | — | Exchange credentials for a token |
| `POST` | `/api/v1/auth/logout` | yes | Revoke the presented token |
| `GET` | `/api/v1/me` | yes | The authenticated user's profile |
| `GET` | `/api/v1/notes` | yes | List your notes — see query parameters below |
| `POST` | `/api/v1/notes` | yes | Create a note |
| `GET` | `/api/v1/notes/:id` | yes | Read a note you own |
| `PUT` | `/api/v1/notes/:id` | yes | Update a note, all fields optional |
| `DELETE` | `/api/v1/notes/:id` | yes | Soft-delete a note |
| `GET` | `/api/v1/tags` | yes | Every tag used across your notes |
| `POST` | `/api/v1/notes/:id/share` | yes | Mint a public, read-only share link |
| `GET` | `/s/:share_token` | — | Read a shared note. No `/api/v1` prefix — public and unauthenticated |

#### Listing query parameters

| Parameter | Default | Notes |
|---|---|---|
| `q` | — | Full-text search across title and body |
| `tag` | — | Filter to one tag; case-insensitive |
| `page` | `1` | |
| `limit` | `20` | Maximum 100 |
| `sort` | `-updated_at` | `created_at` or `updated_at`, `-` prefix for descending |

```
GET /api/v1/notes?q=milk&tag=shopping&page=2&limit=10&sort=-created_at
```

```json
{
  "data": [ { "id": "...", "title": "...", "tags": ["shopping"], "...": "..." } ],
  "meta": { "page": 2, "limit": 10, "total": 34 }
}
```

`meta.total` is the size of the whole result set, not of the page, so a client
can work out how many pages there are.

#### Sharing

```bash
curl -X POST localhost:8080/api/v1/notes/$NOTE_ID/share \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"expires_in": 86400, "password": "let-me-in"}'
```

```json
{ "share_id": "68b7...", "url": "http://localhost:8080/s/9f2a...", "expires_at": "2026-09-04T00:00:00Z" }
```

Both fields are optional; an empty body mints a link with no expiry and no
password. `share_id` is the share record's own id — the token only ever
appears embedded in `url`, never as a second top-level field.

Reading it back needs no `Authorization` header. If the share is password
protected, the password goes in an `X-Share-Password` header rather than a
query parameter, since a query string ends up in access logs, browser history
and `Referer` headers, none of which a password belongs in.

```bash
curl localhost:8080/s/9f2a... -H 'X-Share-Password: let-me-in'
```

The response is a read-only projection — title, body, tags, timestamps — with
no id, owner or `is_public` field, so a visitor holding a valid link learns
nothing about the account behind it. A note that the owner has since deleted
stops resolving through every one of its share links automatically: the note
lookup behind the scenes filters out soft-deleted notes, so there is nothing
extra to clean up when a note is removed.

### Errors

Every failure returns the same shape:

```json
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "Request validation failed",
    "details": { "Email": "must be a valid email address" }
  }
}
```

Status codes in use: 200, 201, 204, 400, 401, 403, 404, 429, 500.

### Example

```bash
curl -X POST localhost:8080/api/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ada","email":"ada@example.com","password":"correct-horse"}'

TOKEN=$(curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct-horse"}' | jq -r .token)

curl localhost:8080/api/v1/me -H "Authorization: Bearer $TOKEN"
```

## Security notes

- Passwords are hashed with bcrypt at cost 12 and the plaintext is never stored.
- Login returns an identical `401` for an unknown address and a wrong password,
  so the endpoint cannot be used to discover which emails have accounts.
- The JWT keyfunc pins HS256, so a token claiming `alg: none` is rejected.
- Logout is real. Each token carries a unique `jti`; logging out records that
  `jti` in a `revoked_tokens` collection which the auth middleware consults on
  every request. A TTL index deletes each entry once the token would have
  expired anyway, so the collection stays bounded.
- If the revocation check cannot reach the database the request is refused
  rather than allowed, so logout fails closed.
- Every note endpoint resolves ownership through one shared code path, so the
  rule cannot drift between verbs. Reading, updating or deleting somebody
  else's note returns `403`; an unknown ID returns `404`.
- Notes are soft-deleted, so a deleted note is recoverable and a share link
  pointing at one still resolves to a document rather than a dangling
  reference.
- The `sort` parameter is matched against an allowlist rather than passed
  through to MongoDB, so a caller cannot sort by a field the API does not
  expose, and every sort is one the indexes support.
- Listing is scoped by `owner_id` inside the query itself, not by filtering
  results afterwards.
- A share-link password goes in a request header, never a query parameter, to
  keep it out of logs, browser history and `Referer` headers.
- A share's password is checked, and the note it points to is confirmed to
  still exist, before the click counter increments — a wrong password or a
  dangling reference is never counted as a read.
- `share_id` in the create-share response is the share's own database id, not
  the token. The token is exposed exactly once, embedded in `url`.
- Every panic is caught and returned in the same error envelope as every other
  failure, rather than Gin's bare 500 with no body.
- No proxy is trusted by default (`SetTrustedProxies(nil)`), so a caller
  cannot spoof the IP address the rate limiter keys on via a forged
  `X-Forwarded-For` header.
- `RATE_LIMIT_REQUESTS`/`RATE_LIMIT_WINDOW` apply to exactly the two surfaces
  api_spec.md's security notes name: the auth endpoints and public share
  access — both reachable with no token at all.
- CORS defaults to a wildcard origin, which is safe specifically because this
  API authenticates with a bearer token rather than a cookie; wildcard CORS
  alongside cookie-based auth would be a different, much riskier story.
- The Docker image is built in two stages, so nothing in the final image is
  the Go toolchain or source tree — only the compiled binary. It runs as a
  non-root user, and no secret is baked into any layer; every setting arrives
  at container start, from the environment.

## Testing

```bash
go test ./...          # everything
go test -race ./...     # with the race detector — clean
```

Every handler-level test runs against a fake, in-memory store through the
real Gin router and, where relevant, the real `RequireAuth` middleware — so
routing, JSON binding, validation, ownership rules, status codes and the
error envelope are all exercised without MongoDB. [tests/auth_test.go](tests/auth_test.go),
[tests/notes_access_test.go](tests/notes_access_test.go),
[tests/shares_access_test.go](tests/shares_access_test.go) and
[tests/middleware_test.go](tests/middleware_test.go) cover the four handler
groups; `internal/utils` and `internal/config` carry their own unit tests
alongside their source, in the usual Go convention.

**What this does not prove:** none of it has run against a real MongoDB. The
bson tags, the five index specifications, `$text` search, `Distinct`,
`FindOneAndUpdate`'s `ReturnDocument(After)`, and the unique index shares'
whole design depends on have been verified by reading, not by running. See
`~/Desktop/evernote_explain.txt` (or ask the maintainer) for the full list of
what remains open.

## Deployment

[Dockerfile](Dockerfile) and [deploy/docker-compose.yml](deploy/docker-compose.yml)
cover local development. [deploy/ecs-task-definition.json](deploy/ecs-task-definition.json)
and [deploy/DEPLOYMENT.md](deploy/DEPLOYMENT.md) walk through a real AWS ECS
Fargate deployment — building and pushing the image to ECR, storing
`JWT_SECRET` and `MONGO_URI` in Secrets Manager rather than as plain
environment values, and the one code change ([app.go](internal/app/app.go)'s
`SetTrustedProxies` call) that needs revisiting once a load balancer sits in
front of the service. None of those steps have been run against a real AWS
account from here — they touch billing and shared infrastructure, so they are
written to be run and checked by a person, not executed automatically.

`scripts/init-indexes.sh` creates the same indexes the application already
creates on every boot, standalone, for pre-warming a production database
ahead of a deploy — see the script's own header for why this is optional
rather than required.

