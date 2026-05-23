FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 go build -o /out/rokid-hermes-connector .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/rokid-hermes-connector /app/rokid-hermes-connector
COPY config /app/config
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/app/rokid-hermes-connector"]
