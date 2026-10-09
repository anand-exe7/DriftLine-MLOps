# Build context is the repo root (go.mod lives there, and the binary imports proto/).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY proto/ proto/
COPY go/ go/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./go/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
