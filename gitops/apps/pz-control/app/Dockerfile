# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/pz-control .

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/pz-control /pz-control
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/pz-control"]
