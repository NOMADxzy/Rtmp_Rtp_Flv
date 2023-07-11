# Version 0.1

# 基础镜像
FROM golang:1.20

# 维护者信息
MAINTAINER zuyunxu@bupt.edu.cn

#镜像操作命令
WORKDIR /home/app

RUN apt update && apt install git

RUN git clone https://github.com/NOMADxzy/GoRtp.git && mv GoRtp/src/net/rtp /usr/local/go/src/net && go build net/rtp && \
    go install net/rtp

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN cd server && go build -v -o /usr/local/bin/app ./

EXPOSE 8090 4242 1935

#容器启动命令
CMD ["app"]
