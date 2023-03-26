# 云端节点

![system.png](https://s2.loli.net/2022/10/04/q2GfX9DdxPhsACH.png)

## 功能
- 接收rtmp推流，将数据封装成`flv Tag`格式
- 将`flv Tag`设为rtp的载荷，通过rtp协议以`组播/单播`方式发送到边缘节点，超过载荷长度则分片发送
- 缓存一定量的rtp包再云端节点，为边缘节点提供quic重传服务

### 涉及的传输协议
- [RTMP](https://github.com/melpon/rfc/blob/master/rtmp.md)
- [RTP](https://www.rfc-editor.org/rfc/rfc3550.html)
- [QUIC](https://datatracker.ietf.org/doc/html/rfc9000)



## 安装

### 使用预编译的可执行文件
[Releases](https://github.com/NOMADxzy/Rtp_Http_Flv/releases)

### 从源码编译
由于依赖了`net/rtp`，所以需编译[GoRtp](https://github.com/wernerd/GoRTP)库
1. 下载源码 `git clone https://github.com/NOMADxzy/Rtmp_Rtp_Flv.git`
2. `cd server` && `go build ./`



## 使用

### 1. 启动云端节点
监听rtmp`1935`端口`./cloudserver`

### 2. 启动[边缘节点](https://github.com/NOMADxzy/Rtp_Http_Flv)
接收云端节点发过来的rtp流，并提供httpflv服务：`./edgeserver -udp_addr :5222`

### 3. 使用`ffmpeg`等工具推流到云端节点

参考命令：`ffmpeg -re -i caton.mp4 -vcodec libx264 -acodec aac -f flv  rtmp://127.0.0.1:1935/live/movie`

### 4. 启动[flv.js播放器](http://bilibili.github.io/flv.js/demo/)

输入播放地址播放：`http://127.0.0.1:7001/live/movie.flv`

### 主要参数配置
`config.yaml`

```bash
rtp_cache_size: 5000 #云端节点缓存的rtp数量
quic_addr: :4242     #quic服务的监听地址
client_addr_list:    #边缘节点的udp及端口号，向这些地址发送数据
- 127.0.0.1:5222
- 127.0.0.1:5224
enable_record: false
rtp_port: 5220
rtmp_addr: :1935
api_addr: :8090
```



## 整体结构

### 代码结构

- `defines.go`：基本配置项文件，包括flv格式用到的常量和rtp缓存、监听地址等参数
- `conn.go`：quic 流对象，用于重传丢失的 rtp 数据包
- `flv.go`：处理 flv 数据，包括构造 flvTag 以及读写 flv 数据
- `listQueue.go`：缓存 rtp 数据包的队列，通过arraylist实现【当前使用】，对外接口和 mapQueue 一致
- `mapQueue.go`：缓存 rtp 数据包的队列，通过hashmap实现，对外接口和 listQueue 一致
- `utils.go`：建立udp连接、quic连接等工具方法
- `cloundserver.go`：主要代码入口程序



### Todo

- [ ] 冗余代码块
- [ ] 日志管理
- [ ] server 文件夹整理



## 参考

### [livego](https://github.com/gwuhaolin/livego)
- 一个直播服务器，参考了一些该项目的代码，在其内部添加了一些接口
### [GoRtp](https://github.com/wernerd/GoRTP)
- 一个流行的`Rtp\Rtcp`的协议栈，使用了该包下Rtp的构建和处理方法
### [quic-go](https://github.com/quic-go/quic-go)
- 一个go实现的易上手的QUIC技术栈

