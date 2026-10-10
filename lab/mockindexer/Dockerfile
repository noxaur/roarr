FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY lab/mockindexer ./lab/mockindexer
RUN CGO_ENABLED=0 go build -o /mockindexer ./lab/mockindexer

FROM alpine:3.22
COPY --from=build /mockindexer /usr/local/bin/mockindexer
EXPOSE 9117
ENTRYPOINT ["mockindexer"]
