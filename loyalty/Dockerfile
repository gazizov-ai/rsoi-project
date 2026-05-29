FROM golang:1.23-alpine AS build
WORKDIR /src
COPY common ./common
COPY loyalty ./loyalty
WORKDIR /src/loyalty
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/loyalty ./cmd/loyalty

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/loyalty /app/loyalty
EXPOSE 8050
ENTRYPOINT ["/app/loyalty"]
