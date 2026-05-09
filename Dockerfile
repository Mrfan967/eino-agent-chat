# ---- 构建阶段 ----
FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o awesomeProject ./cmd/server

# ---- 运行阶段 ----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates tzdata && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/awesomeProject .
COPY web ./web
COPY knowledge ./knowledge
COPY image ./image
COPY prompt_config.json .

ENV TZ=Asia/Shanghai

EXPOSE 8080

CMD ["./awesomeProject"]
