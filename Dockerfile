# Dockerfile for running the 'failures' command, this requires some dependencies
# so using docker to manage those.
# This is not needed for running the server, that does not require these deps.
# Build using: `docker build -t youtupedia:latest .`
# Run using: `docker run --name youtupedia-failures -e "YT_KEY=YT_KEY_HERE" -e "POSTGRES_DSN=user=postgres password=password dbname=youtupedia sslmode=disable" --network=host youtupedia:latest`

FROM golang:1.20

WORKDIR /app

COPY go.mod /app
COPY go.sum /app
COPY cmd /app/cmd
COPY internal /app/internal

RUN go mod download && \
    go build -o /youtupedia -ldflags="-s -w" cmd/youtupedia/main.go

WORKDIR /whisper

# whisper
RUN git clone https://github.com/ggerganov/whisper.cpp . && \
    bash models/download-ggml-model.sh base.en && \
    make && \
    mv main /whisper.cpp && \
    mv models/ggml-base.en.bin /ggml-base.en.bin

ENV YT_KEY=
ENV POSTGRES_DSN=
ENV WHISPER_BIN=/whisper.cpp
ENV WHISPER_MODEL=/ggml-base.en.bin

WORKDIR /

# ffmpeg
RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg && \
    rm -rf /var/lib/apt/lists/*

# yt-dlp
RUN wget --progress=dot https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -O /usr/local/bin/yt-dlp && \
    chmod a+rx /usr/local/bin/yt-dlp

RUN rm -rf /app && \
    rm -rf /whisper

ENTRYPOINT ["/youtupedia", "failures"]
