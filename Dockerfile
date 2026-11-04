FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod .
COPY . .
RUN go test ./... && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /config-service .
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /config-service /config-service
COPY config /config
ENV CONFIG_DIR=/config
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=4s --retries=10 CMD ["/config-service", "healthcheck"]
ENTRYPOINT ["/config-service"]
