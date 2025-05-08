FROM cgr.dev/chainguard/go:latest AS builder
ENV GOOS=linux
ENV CGO_ENABLED=0
ENV GO111MODULE=on
COPY . /src
WORKDIR /src
RUN go mod download
RUN go build -a -installsuffix cgo -o /bin/zrooms ./cmd/zrooms

FROM cgr.dev/chainguard/static:latest
WORKDIR /app
COPY --from=builder /bin/zrooms /app/zrooms
ENTRYPOINT ["/app/zrooms"]