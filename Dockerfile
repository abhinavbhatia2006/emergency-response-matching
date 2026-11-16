# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/coordinator ./cmd/coordinator && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/storage-node ./cmd/storage-node && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/request-api ./cmd/request-api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/matching-worker ./cmd/matching-worker

FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json web/.npmrc ./
RUN npm ci
COPY web ./
RUN npm run build

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 app && adduser -S -D -H -u 10001 -G app app
WORKDIR /app
USER 10001:10001

FROM runtime AS coordinator
COPY --from=go-build /out/coordinator /app/coordinator
COPY config/nodes.container.json /app/config/nodes.container.json
ENTRYPOINT ["/app/coordinator"]
CMD ["-addr", "0.0.0.0:8081", "-nodes", "/app/config/nodes.container.json"]

FROM runtime AS storage-node
COPY --from=go-build /out/storage-node /app/storage-node
ENTRYPOINT ["/app/storage-node"]

FROM runtime AS matching-worker
COPY --from=go-build /out/matching-worker /app/matching-worker
ENTRYPOINT ["/app/matching-worker"]

FROM runtime AS request-api
COPY --from=go-build /out/request-api /app/request-api
COPY --from=web-build /src/web/dist /app/web/dist
ENTRYPOINT ["/app/request-api"]
CMD ["-addr", "0.0.0.0:8080", "-web", "/app/web/dist"]
