FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /webx ./cmd/webx
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /webxd ./cmd/webxd

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /webx /usr/local/bin/webx
VOLUME /data
ENV WEBX_INDEX_DB=/data/index.db
EXPOSE 8080
ENTRYPOINT ["webx"]
CMD ["serve", "--addr", ":8080"]

# webxd — durable edition; multi-worker job claims via Postgres SKIP LOCKED.
# Render (--render/--auto-render) needs a Chrome host or WEBX_RENDER_URL —
# these images are intentionally Chrome-free.
FROM alpine:3.21 AS webxd
RUN apk add --no-cache ca-certificates
COPY --from=build /webxd /usr/local/bin/webxd
EXPOSE 8080
ENTRYPOINT ["webxd"]
CMD ["--addr", ":8080"]
