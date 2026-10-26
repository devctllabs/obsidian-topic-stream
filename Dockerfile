FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/topic-stream ./cmd/topic-stream

FROM alpine:3.23

ARG VERSION=dev
ARG COMMIT=""
LABEL org.opencontainers.image.version=${VERSION} \
      org.opencontainers.image.revision=${COMMIT}

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S topic-stream \
    && adduser -S -G topic-stream topic-stream
COPY --from=build /out/topic-stream /usr/local/bin/topic-stream
USER topic-stream
EXPOSE 7331
ENTRYPOINT ["topic-stream"]
CMD ["serve"]
