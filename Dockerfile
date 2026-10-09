FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/arcane-pangolin-sync ./cmd/arcane-pangolin-sync

FROM alpine:3.20
RUN adduser -D -H -u 10001 app
COPY --from=build /out/arcane-pangolin-sync /usr/local/bin/arcane-pangolin-sync
USER app
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/arcane-pangolin-sync"]

