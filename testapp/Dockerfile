# Build the testapp SUT. Build context is the repo root (the binary imports
# internal/ packages), so build with:
#   docker build -f testapp/Dockerfile -t testapp:dev .
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/testapp ./testapp

# Minimal, non-root runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/testapp /testapp
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/testapp"]
