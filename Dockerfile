# 1) Build the React client.
FROM node:22-alpine AS web
ARG NPM_REGISTRY=https://registry.npmjs.org
WORKDIR /src/web
RUN npm config set registry "$NPM_REGISTRY" && npm install -g pnpm@10.33.4
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 2) Build the Go binaries with web/dist embedded (pure Go SQLite, no CGO).
FROM golang:1.25-alpine AS go
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /src
ENV CGO_ENABLED=0 GOPROXY=$GOPROXY
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN go build -tags embed -trimpath -ldflags="-s -w" -o /out/gem-server ./cmd/gem-server \
 && go build -trimpath -ldflags="-s -w" -o /out/gem-cli ./cmd/gem-cli

# 3) Minimal runtime; the SQLite database lives on the /data volume.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 gem \
 && mkdir /data && chown gem /data
COPY --from=go /out/gem-server /out/gem-cli /usr/local/bin/
USER gem
VOLUME /data
ENV GEM_ADDR=:8080 GEM_DB=/data/gem.db
EXPOSE 8080
# Set GEM_JWT_SECRET at runtime so logins survive restarts:
#   docker run -p 8080:8080 -v gem-data:/data -e GEM_JWT_SECRET=... gem-server
ENTRYPOINT ["gem-server"]
