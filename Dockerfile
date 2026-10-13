# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/playfabd ./cmd/playfabd

FROM gcr.io/distroless/static
COPY --from=build /out/playfabd /playfabd
USER nonroot:nonroot
EXPOSE 22650 22651
ENTRYPOINT ["/playfabd"]
