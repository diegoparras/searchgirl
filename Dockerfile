# syntax=docker/dockerfile:1

# ---- build: a single static binary, no CGO ----
# Base pinneada por digest (build reproducible). Actualizar junto con el tag:
#   docker manifest inspect golang:1.26 | grep digest
FROM golang:1.27@sha256:65b6f280bf050ec5af12716857e8ea8439d694dbba8f31ceeb7630670071f2bb AS build
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
