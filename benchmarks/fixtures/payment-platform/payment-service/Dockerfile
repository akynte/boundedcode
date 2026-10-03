FROM golang:1.24 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server
FROM gcr.io/distroless/static
COPY --from=build /out/server /server
ENTRYPOINT ["/server"]
