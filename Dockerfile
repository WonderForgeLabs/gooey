# syntax=docker/dockerfile:1
#
# gooey demo image.
#
# The default command is the reader (the flagship demo). Every other demo ships
# in the same image at /usr/local/bin/<name>; naming one overrides the default:
#
#	docker run -it --rm ghcr.io/wonderforgelabs/gooey            # reader
#	docker run -it --rm ghcr.io/wonderforgelabs/gooey finder /   # any other demo
#
# Demos: probe, demo, propdemo, logview, markuplog, finder, reader, statedemo,
# sysmon. There is deliberately no ENTRYPOINT — that is what lets the demo name
# be passed as the command.
#
# -it is mandatory: these are TUIs and term.Open() needs a real tty. Without it
# the binary exits with "no tty", which is also the image's smoke test.

FROM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Only the demos that belong to the root module. handlers/temporal is a
# separate module and is not built here.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/bin/ \
	./cmd/probe ./cmd/demo ./cmd/propdemo ./cmd/logview ./cmd/markuplog \
	./cmd/finder ./cmd/reader ./cmd/statedemo ./cmd/sysmon

# Two asset layouts, because the demos resolve markup two different ways.
#
# finder, reader and statedemo look under the working directory first and fall
# back to the directory holding the executable, so the assets go beside the
# binaries. markuplog has no executable-relative fallback: it reads
# cmd/markuplog/logview.gooey relative to the working directory unless given a
# path argument. Mirroring the source layout under /opt/gooey serves markuplog
# and is also the first path the other three probe.
RUN set -eux; \
	cp cmd/finder/finder.gooey cmd/statedemo/statedemo.gooey cmd/reader/*.gooey /out/bin/; \
	mkdir -p /out/gooey/cmd/finder /out/gooey/cmd/markuplog /out/gooey/cmd/statedemo /out/gooey/cmd/reader; \
	cp cmd/finder/finder.gooey /out/gooey/cmd/finder/; \
	cp cmd/markuplog/logview.gooey /out/gooey/cmd/markuplog/; \
	cp cmd/statedemo/statedemo.gooey /out/gooey/cmd/statedemo/; \
	cp cmd/reader/*.gooey /out/gooey/cmd/reader/

# distroless/static carries ca-certificates, which the reader needs to fetch
# feeds over HTTPS. The TUIs write raw ANSI, so no terminfo database is needed.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/bin/ /usr/local/bin/

# Copied with ownership so the tree itself is writable: the reader persists
# feeds.opml into the working directory on first run.
COPY --from=build --chown=nonroot:nonroot /out/gooey /opt/gooey
WORKDIR /opt/gooey

ENV TERM=xterm-256color
CMD ["reader"]
