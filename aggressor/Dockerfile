FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aggressor ./cmd/aggressor

FROM alpine:3.20

RUN addgroup -S cachewarden && adduser -S -G cachewarden cachewarden
COPY --from=build /out/aggressor /usr/local/bin/aggressor
USER cachewarden
ENTRYPOINT ["/usr/local/bin/aggressor"]

