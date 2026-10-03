# ---- build ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/raftkv ./cmd/raftkv

# ---- run ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl && adduser -D -u 10001 raft \
    && mkdir /data && chown raft /data
COPY --from=build /out/raftkv /usr/local/bin/raftkv
USER raft
WORKDIR /home/raft
ENV PORT=8080
EXPOSE 8080
# Default: the self-contained demo (3 nodes + dashboard on $PORT).
# docker-compose overrides this to run one real node per container.
CMD ["sh", "-c", "raftkv cluster -port ${PORT} -data /tmp/raftkv"]
