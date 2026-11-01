# Build from the repository root. CGO keeps SQLite available for local runs;
# musl's static libc lets the deployed image contain only the binary and CAs.
FROM golang:1.25-alpine AS build
RUN apk add --no-cache build-base ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
COPY pkg/diagnostics ./pkg/diagnostics
COPY pkg/fileio ./pkg/fileio
COPY pkg/typesafe ./pkg/typesafe
COPY pkg/teamapi/wire ./pkg/teamapi/wire
COPY services/team-api/go.mod services/team-api/go.sum ./services/team-api/
WORKDIR /src/services/team-api
RUN go mod download
COPY services/team-api ./
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -linkmode external -extldflags '-static' -X main.version=${VERSION}" -o /out/team-api ./cmd/team-api \
 && mkdir -p /out/data /out/tmp
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/team-api /team-api
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/tmp /tmp
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/team-api"]
CMD ["serve"]
