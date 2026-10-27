FROM node:22-bookworm-slim AS frontend
WORKDIR /ui
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build && npm test

FROM golang:1.27.3-bookworm AS source
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /ui/dist ./web/dist

FROM source AS verify
CMD ["sh","scripts/check.sh"]

FROM source AS build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/stockflow ./cmd/stockflow

FROM debian:bookworm-slim AS demo
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && useradd -u 10001 -m appuser
WORKDIR /app
COPY --from=build /out/stockflow /app/stockflow
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/stockflow"]
