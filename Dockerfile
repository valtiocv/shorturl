FROM alpine AS base
RUN apk add --no-cache tzdata ca-certificates
ENV TZ=Asia/Shanghai
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime
RUN echo $TZ > /etc/timezone

FROM golang:1.17-alpine AS dependencies
WORKDIR /app
RUN go env -w GO111MODULE="on"
RUN go env -w GOPROXY="https://goproxy.cn,direct"
COPY go.mod go.sum ./
RUN go mod download

FROM dependencies as builder
WORKDIR /app
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o shorturl main.go

FROM scratch
WORKDIR /app
COPY --from=base /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=base /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/shorturl ./
EXPOSE 8080
ENTRYPOINT ["/app/shorturl"]