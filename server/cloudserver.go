package main

import (
	"crypto/tls"
	"fmt"
	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/gwuhaolin/livego/av"
	"github.com/gwuhaolin/livego/configure"
	"github.com/gwuhaolin/livego/protocol/api"
	"github.com/gwuhaolin/livego/protocol/rtmp"
	"math"
	"math/rand"
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
var ChannelMap *hashmap.Map
var Channels *arraylist.List

type StreamInfo struct {
	strLocalIdx uint32
	SSRC        uint32
	channel     string
	flvFile     *File
	timestamp   uint32
	RtpQueue    *queue
}

//创建一个新的频道，相应的录制文件、rtp发送流、rtp缓存队列
func addChannel(channel string) *StreamInfo {
	SSRC += uint32(1)
	//创建SSRC流
	strLocalIdx, _ := rsLocal.NewSsrcStreamOut(&rtp.Address{local.IP, localPort, localPort + 1, localZone}, SSRC, RTP_INITIAL_SEQ)
	ssrcStream := rsLocal.SsrcStreamOutForIndex(strLocalIdx)
	ssrcStream.SetPayloadType(9)
	//创建录制文件
	flvFile := createFlvFile(channel)

	//创建rtp缓存队列
	var rtpQueue = newQueue(5000)

	streamInfo := &StreamInfo{
		strLocalIdx: strLocalIdx,
		SSRC:        SSRC,
		channel:     channel,
		flvFile:     flvFile,
		RtpQueue:    rtpQueue,
	}

	ChannelMap.Put(SSRC, streamInfo)
	Channels.Add(channel)

	return streamInfo
}

type MyMessageHandler struct{}

//自定义流创建方法
func (handler MyMessageHandler) OnStreamCreated(stream *rtmp.Stream) {
	SSRC := addChannel(stream.Channel()).SSRC
	stream.SetSsrc(SSRC)
	fmt.Println("NewStreamCreated SSRC = ", SSRC)

}

//自定义消息处理方法
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
		if err != nil {
			panic(err)
		}

		videoDataSize += int64(len(message.Data))
	} else if message.IsAudio {
		//创建flv
		flv_tag = make([]byte, 11+len(tagdata))
		_, err := CreateTag(flv_tag, tagdata, AUDIO_TAG, message.TimeStamp)
		if err != nil {
			panic(err)
		}
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

func sendPacket(rp *rtp.DataPacket) {
	if USE_MULTICAST { //组播
		sendPacketmulticast(rp)
	} else { //单播
		r := rand.Intn(1000)
		if float64(r)/1000.0 >= PACKET_LOSS_RATE {
			_, err := rsLocal.WriteData(rp)
			if err != nil {
				return
			}
		}
	}
}

func sendPacketmulticast(rp *rtp.DataPacket) { //将rtp包发送到组播地址组播
	var err error
	if udpConn == nil {
		udpConn, err = NewBroadcaster(MULTICAST_ADDRASS)
		if err != nil {
			panic(err)
		}
	}
	r := rand.Intn(1000)
	if float64(r)/1000.0 >= PACKET_LOSS_RATE {
		_, err := udpConn.Write(rp.Buffer()[:rp.InUse()])
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

//打印历史信息
func showRecvDataSize() {
	for {
		_ = <-time.After(5 * time.Second)
		fmt.Printf("Audio size: %d bytes; Vedio size: %d bytes\n", audioDataSize, videoDataSize)
	}
}

//启动quic服务
func startQuic() {
	fmt.Println("quic server started on ", QUIC_ADDR)
	conn := initialQUIC()

	//通过channel和seq找到所需的rtp包
	var seq uint16
	var ssrc uint32

	for {
		//fmt.Println("quic线程启动，等待重传序列号")

		err := conn.ReadSsrc(&ssrc)
		if err != nil {
			//长时间收不到重传请求会触发err
			time.Sleep(time.Second)
			continue
		}

		_, err = conn.ReadSeq(&seq)
		if err != nil {
			panic(err)
		}

		fmt.Println("收到重传请求，seq: ", seq)

		//发送rtp数据包给客户
		val, f := ChannelMap.Get(ssrc)
		if !f {
			fmt.Printf("error,can not find streamInfo, ssrc = %d\n", ssrc)
		}
		pkt := val.(*StreamInfo).RtpQueue.GetPkt(seq)
		//fmt.Println(pkt)
		if pkt != nil {
			_, err = conn.SendRtp(pkt)
			if err != nil {
				panic(err)
			}
		} else {
			fmt.Println("quic无法重传，没有该包，seq：", seq)
		}
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
	rsLocal = rtp.NewSession(tpLocal, tpLocal)
	rsLocal.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1, remoteZone})

	rsLocal.StartSession()
	defer rsLocal.CloseSession()

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

	Channels = arraylist.New()

	myMessageHandler := &MyMessageHandler{}
	stream := rtmp.NewRtmpStream(myMessageHandler)
	ChannelMap = hashmap.New()

	go showRecvDataSize()
	go startQuic()

	startAPI(stream)
	startRtmp(stream)

}
