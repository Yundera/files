# syntax=docker/dockerfile:1

# 1) Build the Svelte UI -> internal/ui/dist
FROM node:lts-slim AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build   # writes to /src/internal/ui/dist

# 2) Build the Go binary with the UI embedded
FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /files ./cmd/files

# 3) Minimal runtime.
#
# Deliberately smaller than maison's: no docker-cli, no bash, no hook shell. This
# app never shells out — it only reads and writes files — so the image is the
# binary plus two things it genuinely needs:
#   ca-certificates  nothing outbound today, but a bare image makes any future
#                    HTTPS call fail with an opaque x509 error
#   tzdata           TZ names the timezone shown in the UI; without the database
#                    every zone silently resolves to UTC
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=backend /files /files
EXPOSE 8080
ENTRYPOINT ["/files"]
