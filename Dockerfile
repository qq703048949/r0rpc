FROM golang:1.24-alpine AS builder

# 设置 Go 代理为国内镜像
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/r0rpc-server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/r0rpc-dbinit ./cmd/dbinit

FROM alpine:3.20
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata bash netcat-openbsd
COPY --from=builder /out/r0rpc-server /app/r0rpc-server
COPY --from=builder /out/r0rpc-dbinit /app/r0rpc-dbinit
COPY deploy/linux/start.sh /app/start.sh
RUN chmod +x /app/start.sh
EXPOSE 8080
ENTRYPOINT ["/app/start.sh"]