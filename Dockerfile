FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
# GOPROXY defaults to goproxy.cn; override at build time with --build-arg GOPROXY=https://proxy.golang.org,direct
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server/ \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /worker ./cmd/worker/

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
COPY --from=builder /server /server
COPY --from=builder /worker /worker
# Run as a non-root user
RUN addgroup -S kada && adduser -S kada -G kada
USER kada
EXPOSE 8080
CMD ["/server"]
