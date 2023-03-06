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
var ChannelMap *hashmap.Map //key:SSRC，val:streamInfo
var UdpConns *arraylist.List

type StreamInfo struct {
	strLocalIdx uint32
	SSRC        uint32
	channel     string
	flvFile     *File
	timestamp   uint32
	RtpQueue    *mapQueue
}

// 创建一个新的频道，相应的录制文件、rtp发送流、rtp缓存队列
func addChannel(channel string) *StreamInfo {
	SSRC += uint32(1)
	//创建SSRC流
	strLocalIdx, _ := rsLocal.NewSsrcStreamOut(&rtp.Address{local.IP, localPort, localPort + 1, localZone}, SSRC, RTP_INITIAL_SEQ)
	ssrcStream := rsLocal.SsrcStreamOutForIndex(strLocalIdx)
	ssrcStream.SetPayloadType(77)
	//创建录制文件
	flvFile := createFlvFile(channel)

	//创建rtp缓存队列
	var rtpQueue = newMapQueue(5000)

	streamInfo := &StreamInfo{
		strLocalIdx: strLocalIdx,
		SSRC:        SSRC,
		channel:     channel,
		flvFile:     flvFile,
		RtpQueue:    rtpQueue,
	}

	ChannelMap.Put(SSRC, streamInfo)
	return streamInfo
}

type MyMessageHandler struct{}

// 自定义流创建方法
func (handler MyMessageHandler) OnStreamCreated(stream *rtmp.Stream) {
	SSRC := addChannel(stream.Channel()).SSRC
	stream.SetSsrc(SSRC)
	fmt.Println("NewStreamCreated SSRC = ", SSRC)

}

// 自定义流停止方法
func (handler MyMessageHandler) OnStreamClosed(stream *rtmp.Stream) {
	ChannelMap.Remove(stream.Ssrc())
	fmt.Println("StreamClosed SSRC = ", stream.Ssrc())
}

// 自定义消息处理方法
func (handler MyMessageHandler) OnReceived(s *rtmp.Stream, message *av.Packet) {
	var streamInfo *StreamInfo
	val, f := ChannelMap.Get(s.Ssrc())
	if !f {
		streamInfo = addChannel(s.Channel())
	} else {
		streamInfo = val.(*StreamInfo)
	}

	tagdata := message.Data
	var flv_tag []byte
	streamInfo.timestamp += uint32(1)

	if message.IsVideo {
		//创建flv
		flv_tag = make([]byte, 11+len(tagdata))
		_, err := CreateTag(flv_tag, tagdata, VIDEO_TAG, message.TimeStamp)
		checkError(err)

		videoDataSize += int64(len(message.Data))
	} else if message.IsAudio {
		//创建flv
		flv_tag = make([]byte, 11+len(tagdata))
		_, err := CreateTag(flv_tag, tagdata, AUDIO_TAG, message.TimeStamp)
		checkError(err)
		audioDataSize += int64(len(message.Data))
	} else {
		return
	}

	//发送flv_tag，超长则分片发送
	flv_tag_len := len(flv_tag)
	var rp *rtp.DataPacket
	if flv_tag_len <= MAX_RTP_PAYLOAD_LEN {
		rp = rsLocal.NewDataPacketForStream(streamInfo.strLocalIdx, streamInfo.timestamp)
		rp.SetMarker(true)
		rp.SetPayload(flv_tag)
		sendPacket(rp)

		rtp_buf := make([]byte, rp.InUse()) //复制一份放入map之中
		copy(rtp_buf, rp.Buffer()[:rp.InUse()])
		streamInfo.RtpQueue.Enqueue(rtp_buf, rp.Sequence())
		//fmt.Println(rtp_buf)
		//fmt.Println("当前rtp队列长度：", rtp_queue.queue.Len(), " 队列数据量：", rtp_queue.bytesInQueue)
		rp.FreePacket() //释放内存
	} else {
		slice_num := int(math.Ceil(float64(flv_tag_len) / float64(MAX_RTP_PAYLOAD_LEN)))
		for i := 0; i < slice_num; i++ {
			rp = rsLocal.NewDataPacketForStream(streamInfo.strLocalIdx, streamInfo.timestamp)
			last_slice := i == slice_num-1
			rp.SetMarker(last_slice)
			if !last_slice {
				rp.SetPayload(flv_tag[i*MAX_RTP_PAYLOAD_LEN : (i+1)*MAX_RTP_PAYLOAD_LEN])
			} else {
				rp.SetPayload(flv_tag[i*MAX_RTP_PAYLOAD_LEN:])
			}
			sendPacket(rp)

			rtp_buf := make([]byte, rp.InUse())
			copy(rtp_buf, rp.Buffer()[:rp.InUse()])
			streamInfo.RtpQueue.Enqueue(rtp_buf, rp.Sequence())
			//fmt.Println("当前rtp队列长度：", rtp_queue.queue.Len(), " 队列数据量：", rtp_queue.bytesInQueue)
			rp.FreePacket() //释放内存
		}
	}

	//fmt.Println("rtp seq:", rp.Sequence(), ",payload size: ", len(tagdata)+11, ",rtp timestamp: ", timestamp)
	//fmt.Println(flv_tag)
	err := streamInfo.flvFile.WriteTagDirect(flv_tag)
	if err != nil {
		return
	}
}

//	func sendPacket(rp *rtp.DataPacket) {
//		if USE_MULTICAST { //组播
//			sendPacketmulticast(rp)
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
func sendPacket(rp *rtp.DataPacket) {
	for _, udpConn := range UdpConns.Values() {
		_, err := udpConn.(*net.UDPConn).Write(rp.Buffer()[:rp.InUse()])
		if err != nil {
			return
		}
	}
}

func startRtmp(stream *rtmp.RtmpStream) {
	rtmpAddr := configure.Config.GetString("rtmp_addr")
	isRtmps := configure.Config.GetBool("enable_rtmps")

	var rtmpListen net.Listener
	if isRtmps {
		certPath := configure.Config.GetString("rtmps_cert")
		keyPath := configure.Config.GetString("rtmps_key")
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			log.Fatal(err)
		}

		rtmpListen, err = tls.Listen("tcp", rtmpAddr, &tls.Config{
			Certificates: []tls.Certificate{cert},
		})
		if err != nil {
			log.Fatal(err)
		}
	} else {
		var err error
		rtmpListen, err = net.Listen("tcp", rtmpAddr)
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
		log.Info("RTMPS Listen On ", rtmpAddr)
	} else {
		log.Info("RTMP Listen On ", rtmpAddr)
	}
	rtmpServer.Serve(rtmpListen)
}

func startAPI(stream *rtmp.RtmpStream) {
	apiAddr := configure.Config.GetString("api_addr")
	rtmpAddr := configure.Config.GetString("rtmp_addr")

	if apiAddr != "" {
		opListen, err := net.Listen("tcp", apiAddr)
		if err != nil {
			log.Fatal(err)
		}
		opServer := api.NewServer(stream, rtmpAddr)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("HTTP-API server panic: ", r)
				}
			}()
			log.Info("HTTP-API listen On ", apiAddr)
			opServer.Serve(opListen)
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
func showRecvDataSize() {
	for {
		_ = <-time.After(5 * time.Second)
		fmt.Printf("Audio size: %d bytes; Vedio size: %d bytes\n", audioDataSize, videoDataSize)
	}
}

// 启动quic服务
func startQuic() {
	tlsConf, err := generateTLSConfig()
	if err != nil {
		panic(err)
	}

	ln, err := quic.ListenAddr("localhost:4242", tlsConf, nil)
	checkError(err)

	for {
		fmt.Println("quic server started on ", "localhost"+conf.QUIC_ADDR)
		conn := WaitForQuicConn(ln)
		go conn.Serve()
	}

}

func initUdpConns() {
	UdpConns = arraylist.New()
	for i := 0; i < len(conf.CLIENT_ADDRESS_LIST); i++ {
		addr := conf.CLIENT_ADDRESS_LIST[i]
		newConn, err := NewUDPConn(addr)
		checkError(err)
		UdpConns.Add(newConn)
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("livego panic: ", r)
			time.Sleep(1 * time.Second)
		}
	}()

	log.Infof(`
    (╯°口°)╯( ┴—┴ Rtmp Http FLv （┬_┬）
        version: %s
	`, VERSION)

	tpLocal, _ := rtp.NewTransportUDP(local, localPort, localZone)
	rsLocal = rtp.NewSession(tpLocal, tpLocal) //用来创建rtp包
	//rsLocal.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1, remoteZone})
	//rsLocal.StartSession()
	//defer rsLocal.CloseSession()

	// close flv file
	defer func() {
		vals := ChannelMap.Values()
		for _, val := range vals {
			flvFile := val.(StreamInfo).flvFile
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
	go showRecvDataSize()
	go startQuic()

	startAPI(stream)
	startRtmp(stream)

}
