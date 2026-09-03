# syntax=docker/dockerfile:1

# ---- build stage --------------------------------------------------------
# A separate build stage means the final image ships a compiled binary and
# nothing else: no Go toolchain, no module cache, no source tree. Smaller
# image, and a smaller surface for anything to go wrong at runtime.
FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies are downloaded before the source is copied in, so that editing
# application code does not invalidate this layer and force a re-download on
# every build — only a go.mod/go.sum change does.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produces a statically linked binary, which is what makes it
# possible to run on the minimal base image below — a dynamically linked
# binary would be missing the C libraries it was built against.
RUN CGO_ENABLED=0 GOOS=linux go build -o /evernote-lite ./cmd/evernote-lite

# ---- runtime stage -------------------------------------------------------
# Alpine rather than distroless/scratch: it carries ca-certificates (needed
# for a TLS connection to MongoDB Atlas) and a shell, at the cost of a few MB
# — a reasonable trade for how much easier it makes debugging a running
# container.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 appuser

COPY --from=build /evernote-lite /usr/local/bin/evernote-lite

USER appuser
EXPOSE 8080

# No secret is baked into the image at any stage above — JWT_SECRET,
# MONGO_URI and every other setting arrive at container start, from the
# environment, exactly as internal/config/config.go requires.
ENTRYPOINT ["/usr/local/bin/evernote-lite"]
