FROM golang:1.21 AS build
WORKDIR /src
COPY services/api .
RUN go build ./...

FROM gcr.io/distroless/base AS run
