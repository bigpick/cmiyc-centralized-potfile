# syntax=docker/dockerfile:1

# ---- build stage ------------------------------------------------------------
# Pinned to the Go toolchain the module targets. Bump both this tag and the
# `go` directive in go.mod together.
FROM golang:1.27.1-bookworm AS build

WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 yields a fully static binary so it runs on distroless/static.
# -trimpath strips local paths; -s -w drop the symbol table and DWARF to shrink.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/cmiyc \
        ./cmd/cmiyc

# ---- runtime stage ----------------------------------------------------------
# Distroless static: no shell, no package manager, minimal attack surface, and
# a built-in nonroot user. The server reads PORT/DATABASE_URL/POOL_TOKEN from
# the environment, so no shell-based variable expansion is needed at runtime.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/cmiyc /usr/local/bin/cmiyc

USER nonroot:nonroot

# Cosmetic; Railway assigns and routes $PORT dynamically.
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/cmiyc"]
CMD ["serve"]
