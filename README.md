# Evernote-Lite

A backend REST API for personal note-taking, written in Go. Users sign up, log
in with a JWT, and manage their own notes — creating, editing, tagging,
searching and soft-deleting them. Any note can be turned into a public,
read-only share link that works without an account.

Backend only: there is no frontend. The deliverable is the API, its data model,
and the operational scaffolding around it.

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

## API

Base path `/api/v1`. Protected endpoints require `Authorization: Bearer <token>`.

### Available now

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/healthz` | — | Liveness, including a MongoDB ping |
| `POST` | `/api/v1/auth/signup` | — | Create an account |
| `POST` | `/api/v1/auth/login` | — | Exchange credentials for a token |
| `POST` | `/api/v1/auth/logout` | yes | Revoke the presented token |
| `GET` | `/api/v1/me` | yes | The authenticated user's profile |

### Planned

`GET|POST /notes`, `GET|PUT|DELETE /notes/:id`, `GET /tags`,
`POST /notes/:id/share`, and the public `GET /s/:share_token`.

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

## Roadmap

1. **Foundation and auth** — layout, config, MongoDB, error envelope, bcrypt,
   JWT, auth middleware, signup/login/logout/me. **Complete.**
2. Notes CRUD with ownership enforcement.
3. Listing, full-text search, tag filtering, pagination and sorting.
4. Public share links with an optional password and optional expiry.
5. Logging, CORS and rate-limiting middleware.
6. Tests, Dockerfile and AWS deployment.
