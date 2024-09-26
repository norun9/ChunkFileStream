FROM golang:1.23.1-alpine AS build

ENV ROOT=/go/src/project
WORKDIR ${ROOT}

COPY . ${ROOT}

RUN go mod download \
    && CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:3.20.3

ENV ROOT=/go/src/project
WORKDIR ${ROOT}

RUN addgroup -S dockergroup && adduser -S docker -G dockergroup
USER docker

COPY --from=build ${ROOT}/server ${ROOT}

EXPOSE 80

CMD ["./server"]