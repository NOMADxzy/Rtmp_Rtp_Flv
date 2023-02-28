# CloudServer

![system.png](https://s2.loli.net/2022/10/04/q2GfX9DdxPhsACH.png)

## Start

### 单播
> defines.go 文件中 `USE_MULTICAST = false`
>
> Local: rtpPort = 5220, rtcpPort = 5221
>
> Remote: rtpPort = 5222, rtcpPort = 5223
- `go run cloudserver.go`



### 启用组播
> defines.go 文件中 `USE_MULTICAST = true`
>
> MULTICAST_ADDRESS   = "239.0.0.0:5222"

- `go run cloudserver.go`



## Configuration

```go
const (
	AUDIO_TAG           = byte(0x08)
	VIDEO_TAG           = byte(0x09)
	SCRIPT_DATA_TAG     = byte(0x12)
	DURATION_OFFSET     = 53
	HEADER_LEN          = 13
	MAX_RTP_PAYLOAD_LEN = 1000	// RTP payload 载荷，过大需要分片，一般是 video metadat
	PACKET_LOSS_RATE    = 0.00
	MULTICAST_ADDRESS   = "239.0.0.0:5222"
	QUIC_ADDR           = "localhost:4242"
	USE_MULTICAST       = false
	RTP_INITIAL_SEQ     = uint16(65000)	// 初始 RTP sequence number，最大为 65535，rtp 库自动维护
)

var localPort = 5220
var local, _ = net.ResolveIPAddr("ip", "127.0.0.1")

var remotePort = 5222
var remote, _ = net.ResolveIPAddr("ip", "127.0.0.1")

var localZone = ""
var remoteZone = ""

// 发送单播数据包
var rsLocal *rtp.Session

// 发送组播数据包
var udpConn *net.UDPConn
```



## Structure

- `define.go`：基本配置项文件，包括 local，remote 端口以及发送 rtp 组播数据包的函数
- `conn.go`：quic 服务器，用于重传丢失的 rtp 数据包
- `flv.go`：处理 flv 数据，包括构造 flvTag 以及读写 flv 数据
- `rtp_packet.go`：处理 rtp 数据包，包括设置 ssrc 等
- `rtp_queue.go`：缓存 rtp 数据包的 linkedHashMap
- `cloundserver.go`：代码主要逻辑
