# syntax=docker/dockerfile:1

# ---- build: a single static binary, no CGO ----
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -ldflags="-s -w" -o /searchgirl ./cmd/searchgirl

# ---- runtime: scratch + CA roots ----
# A diferencia de COGO, Searchgirl hace TLS saliente (url_read, LLM remotos):
# sin los CA certificates, toda conexión https fallaría con x509 errors.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /searchgirl /searchgirl
EXPOSE 8080
# Default: servicio HTTP largo (UI + API + MCP en /mcp).
# Para MCP por stdio local:  docker run -i searchgirl serve
ENTRYPOINT ["/searchgirl"]
CMD ["serve", "-http", ":8080"]
