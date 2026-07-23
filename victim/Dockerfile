FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/victim ./cmd/victim

FROM alpine:3.20

RUN addgroup -S cachewarden && adduser -S -G cachewarden cachewarden
COPY --from=build /out/victim /usr/local/bin/victim
USER cachewarden
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/victim"]

