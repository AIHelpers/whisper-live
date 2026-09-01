# whisper-live server image.
#
# Spec section 10: "server mode as a Docker image reusing the existing
# docker-whisper-tiny base for parity." This repo doesn't have access to
# that image's Dockerfile, so this build uses a standard Go multi-stage
# build for the whisper-live binary itself; point -transcriber=docker
# at your docker-whisper-tiny image name (mounting the host Docker socket
# in) to get that parity, or bake whisper.cpp's CLI + a ggml model into
# this image and run with -transcriber=whispercpp for in-container
# inference without a docker-in-docker dependency.

FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/whisperlive-server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/whisperlive-server ./whisperlive-server
COPY frontend/web ./frontend/web

ENV WHISPERLIVE_ADDR=:8080
ENV WHISPERLIVE_TRANSCRIBER=mock
EXPOSE 8080

ENTRYPOINT ["./whisperlive-server"]
