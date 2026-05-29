FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY identity ./identity
WORKDIR /src/identity
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/identity ./cmd/identity

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/identity /app/identity
EXPOSE 8084
ENTRYPOINT ["/app/identity"]
