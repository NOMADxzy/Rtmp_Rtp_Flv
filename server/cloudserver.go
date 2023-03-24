package main

import (
	"crypto/tls"
	"fmt"
	"github.com/NOMADxzy/livego/av"
	"github.com/NOMADxzy/livego/configure"
	"github.com/NOMADxzy/livego/protocol/api"
	"github.com/NOMADxzy/livego/protocol/rtmp"
	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/quic-go/quic-go"
	"math"
	"net"
	"net/rtp"
	"path"
	"runtime"
	"time"

	"github.com/emirpasic/gods/maps/hashmap"
	log "github.com/sirupsen/logrus"
)

var videoDataSize int64
var audioDataSize int64
var VERSION = "master"
var SSRC = uint32(1020303)
var ChannelMap *hashmap.Map // key:SSRC，val:streamEntity
var UdpConns *arraylist.List

type StreamEntity struct {
	strLocalIdx uint32
	SSRC        uint32
	channel     string
	flvFile     *File
	timestamp   uint32
	RtpQueue    *listQueue
}

// 创建一个新的频道，相应的录制文件、rtp发送流、rtp缓存队列
func addChannel(channel string) *StreamEntity {
	SSRC += uint32(1)
	//创建SSRC流
	strLocalIdx, _ := rsLocal.NewSsrcStreamOut(&rtp.Address{
		IPAddr: local.IP, DataPort: conf.RTP_PORT, CtrlPort: conf.RTP_PORT + 1, Zone: localZone}, SSRC, RTP_INITIAL_SEQ)
	ssrcStream := rsLocal.SsrcStreamOutForIndex(strLocalIdx)
	ssrcStream.SetPayloadType(77)
	//创建录制文件
	var flvFile *File
	if conf.ENABLE_RECORD {
		flvFile = createFlvFile(channel)
		fmt.Println("Create record file path = ", "/", channel+".flv")
	}

	//创建rtp缓存队列
	var rtpQueue = newlistQueue(conf.RTP_CACHE_SIZE, SSRC)
	go rtpQueue.printInfo()

	streamEntity := &StreamEntity{
		strLocalIdx: strLocalIdx,
		SSRC:        SSRC,
		channel:     channel,
		flvFile:     flvFile,
		RtpQueue:    rtpQueue,
	}

	ChannelMap.Put(SSRC, streamEntity)
	return streamEntity
}

type MyMessageHandler struct{}

// OnStreamCreated 自定义流创建方法
func (handler MyMessageHandler) OnStreamCreated(stream *rtmp.Stream) {
	SSRC := addChannel(stream.Channel()).SSRC
	stream.SetSsrc(SSRC)
	fmt.Println("NewStreamCreated SSRC = ", SSRC)
}

// OnStreamClosed 自定义流停止方法
func (handler MyMessageHandler) OnStreamClosed(stream *rtmp.Stream) {
	if val, ok := ChannelMap.Get(stream.Ssrc()); ok {
		streamEntity := val.(*StreamEntity)
		streamEntity.flvFile.Close()
		streamEntity.RtpQueue.Closed = true
	}
	ChannelMap.Remove(stream.Ssrc())
	fmt.Println("StreamClosed SSRC = ", stream.Ssrc())
}

// OnReceived 自定义消息处理方法
func (handler MyMessageHandler) OnReceived(s *rtmp.Stream, message *av.Packet) {
	var streamEntity *StreamEntity
	val, f := ChannelMap.Get(s.Ssrc())
	if !f {
		streamEntity = addChannel(s.Channel())
	} else {
		streamEntity = val.(*StreamEntity)
	}

	// metaData 相当于 flvTagBody
	metadata := message.Data
	var flvTag []byte
	streamEntity.timestamp += uint32(1)

	if message.TimeStamp == 0 {
		s.StartTime = time.Now().UnixMilli()
	}

	// 创建音频或视频 flvTag = flvTagHeader (11 bytes) + flvTagBody
	if message.IsVideo {
		flvTag = make([]byte, 11+len(metadata))
		_, err := CreateTag(flvTag, metadata, VIDEO_TAG, message.TimeStamp)
		checkError(err)
		videoDataSize += int64(len(message.Data))
	} else if message.IsAudio {
		flvTag = make([]byte, 11+len(metadata))
		_, err := CreateTag(flvTag, metadata, AUDIO_TAG, message.TimeStamp)
		checkError(err)
		audioDataSize += int64(len(message.Data))
	} else {
		return
	}

	// 发送flv_tag，超长则分片发送
	flv_tag_len := len(flvTag)
	var rp *rtp.DataPacket
	if flv_tag_len <= MAX_RTP_PAYLOAD_LEN {
		rp = rsLocal.NewDataPacketForStream(streamEntity.strLocalIdx, streamEntity.timestamp)
		rp.SetMarker(true)
		rp.SetPayload(flvTag)
		sendPacket(rp)

		rtp_buf := make([]byte, rp.InUse()) // 深拷贝：需要提前复制
		copy(rtp_buf, rp.Buffer()[:rp.InUse()])
		streamEntity.RtpQueue.Enqueue(rtp_buf, rp.Sequence()) // 加入 RtpQueue
		rp.FreePacket()                                       // 释放内存
	} else {
		slice_num := int(math.Ceil(float64(flv_tag_len) / float64(MAX_RTP_PAYLOAD_LEN)))
		for i := 0; i < slice_num; i++ {
			rp = rsLocal.NewDataPacketForStream(streamEntity.strLocalIdx, streamEntity.timestamp)
			last_slice := i == slice_num-1
			rp.SetMarker(last_slice)
			if !last_slice {
				rp.SetPayload(flvTag[i*MAX_RTP_PAYLOAD_LEN : (i+1)*MAX_RTP_PAYLOAD_LEN])
			} else {
				rp.SetPayload(flvTag[i*MAX_RTP_PAYLOAD_LEN:])
			}
			sendPacket(rp)

			rtp_buf := make([]byte, rp.InUse())
			copy(rtp_buf, rp.Buffer()[:rp.InUse()])
			streamEntity.RtpQueue.Enqueue(rtp_buf, rp.Sequence())
			rp.FreePacket()
		}
	}

	//fmt.Println("rtp seq:", rp.Sequence(), ",payload size: ", len(metadata)+11, ",rtp timestamp: ", timestamp)
	//fmt.Println(flv_tag)
	if streamEntity.flvFile != nil {
		err := streamEntity.flvFile.WriteTagDirect(flvTag)
		checkError(err)
	}
}

//	func sendPacket(rp *rtp.DataPacket) {
//		if USE_MULTICAST { //组播
//			sendPacketMulticast(rp)
//		} else { //单播
//			r := rand.Intn(1000)
//			if float64(r)/1000.0 >= PACKET_LOSS_RATE {
//				_, err := rsLocal.WriteData(rp)
//				if err != nil {
//					return
//				}
//			}
//		}
//	}

// 枚举所有的 UdpConns 列表，发送当前 rp.Buffer()[:rp.InUse()] 数据
func sendPacket(rp *rtp.DataPacket) {
	for _, udpConn := range UdpConns.Values() {
		_, err := udpConn.(*net.UDPConn).Write(rp.Buffer()[:rp.InUse()])
		checkError(err)
	}
}

func startRtmp(stream *rtmp.RtmpStream) {
	RtmpAddr := conf.RTMP_ADDR
	isRtmps := configure.Config.GetBool("enable_rtmps")

	var rtmpListen net.Listener
	if isRtmps {
		certPath := configure.Config.GetString("rtmps_cert")
		keyPath := configure.Config.GetString("rtmps_key")
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			log.Fatal(err)
		}

		rtmpListen, err = tls.Listen("tcp", RtmpAddr, &tls.Config{
			Certificates: []tls.Certificate{cert},
		})
		if err != nil {
			log.Fatal(err)
		}
	} else {
		var err error
		rtmpListen, err = net.Listen("tcp", RtmpAddr)
		if err != nil {
			log.Fatal(err)
		}
	}

	var rtmpServer *rtmp.Server

	rtmpServer = rtmp.NewRtmpServer(stream, nil)

	defer func() {
		if r := recover(); r != nil {
			log.Error("RTMP server panic: ", r)
		}
	}()
	if isRtmps {
		log.Info("RTMPS Listen On ", RtmpAddr)
	} else {
		log.Info("RTMP Listen On ", RtmpAddr)
	}
	err := rtmpServer.Serve(rtmpListen)
	checkError(err)
}

func startAPI(stream *rtmp.RtmpStream) {
	apiAddr := conf.API_ADDR
	if apiAddr != "" {
		opListen, err := net.Listen("tcp", apiAddr)
		if err != nil {
			log.Fatal(err)
		}
		opServer := api.NewServer(stream, conf.RTMP_ADDR)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("HTTP-API server panic: ", r)
				}
			}()
			log.Info("HTTP-API listen On ", apiAddr)
			err = opServer.Serve(opListen)
			checkError(err)
		}()
	}
}

func init() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := path.Base(f.File)
			return fmt.Sprintf("%s()", f.Function), fmt.Sprintf(" %s:%d", filename, f.Line)
		},
	})
}

// 打印历史信息
//func showRecvDataSize() {
//	for {
//		_ = <-time.After(5 * time.Second)
//		fmt.Printf("Audio size: %d bytes; Video size: %d bytes\n", audioDataSize, videoDataSize)
//	}
//}

// 启动quic服务
func startQuic() {
	tlsConf, err := generateTLSConfig()
	checkError(err)
	ln, err := quic.ListenAddr("0.0.0.0"+conf.QUIC_ADDR, tlsConf, nil)
	fmt.Println("quic server listening on ", "0.0.0.0"+conf.QUIC_ADDR)
	checkError(err)

	for {
		quicConn := WaitForQuicConn(ln)
		go quicConn.Serve()
	}
}

// 为不同的边缘节点初始化 udpConn
func initUdpConns() {
	UdpConns = arraylist.New()
	for i := 0; i < len(conf.CLIENT_ADDR_LIST); i++ {
		addr := conf.CLIENT_ADDR_LIST[i]
		udpConn, err := NewUDPConn(addr)
		checkError(err)
		UdpConns.Add(udpConn)
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("Rtmp Http FLv panic: ", r)
			time.Sleep(1 * time.Second)
		}
	}()

	log.Infof(`
    (╯°口°)╯( ┴—┴ Rtmp Http FLv （┬_┬）
        version: %s
	`, VERSION)

	tpLocal, _ := rtp.NewTransportUDP(local, conf.RTP_PORT, localZone)
	rsLocal = rtp.NewSession(tpLocal, tpLocal) //用来创建rtp包
	//rsLocal.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1, remoteZone})
	//rsLocal.StartSession()
	//defer rsLocal.CloseSession()

	// close flv file
	defer func() {
		for _, val := range ChannelMap.Values() {
			flvFile := val.(StreamEntity).flvFile
			if flvFile != nil {
				flvFile.Close()
			}
		}
	}()

	myMessageHandler := &MyMessageHandler{}
	stream := rtmp.NewRtmpStream(myMessageHandler)
	ChannelMap = hashmap.New()

	conf.readFromXml("./config.yaml")
	initUdpConns()
	//go showRecvDataSize()
	go startQuic()

	startAPI(stream)
	startRtmp(stream)
}
