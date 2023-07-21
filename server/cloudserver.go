package main

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"github.com/NOMADxzy/livego/av"
	"github.com/NOMADxzy/livego/configure"
	"github.com/NOMADxzy/livego/protocol/api"
	"github.com/NOMADxzy/livego/protocol/rtmp"
	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/quic-go/quic-go"
	"io"
	"math"
	"net"
	"net/rtp"
	"os"
	"strconv"
	"time"

	"github.com/emirpasic/gods/maps/hashmap"
	logrus "github.com/sirupsen/logrus"
	"rtmp_rtp_flv/sr"
)

var videoDataSize int64
var audioDataSize int64
var VERSION = "master"
var SSRC = uint32(1020303)
var ChannelMap *hashmap.Map // key:SSRC，val:streamEntity
var UdpConns *arraylist.List
var running bool
var seqBytes []byte

type StreamEntity struct {
	strLocalIdx uint32
	SSRC        uint32
	channel     string
	flvFile     *File
	timestamp   uint32
	RtpQueue    *listQueue
	NextSeq     uint16
}

// 创建一个新的频道，相应的录制文件、rtp发送流、rtp缓存队列
func addChannel(channel string) *StreamEntity {
	SSRC += uint32(1)
	//创建SSRC流
	strLocalIdx, _ := rsLocal.NewSsrcStreamOut(&rtp.Address{
		IPAddr: local.IP, DataPort: conf.RTP_PORT, CtrlPort: conf.RTP_PORT + 1, Zone: localZone}, SSRC, RTP_INITIAL_SEQ)
	ssrcStream := rsLocal.SsrcStreamOutForIndex(strLocalIdx)
	ssrcStream.SetPayloadType(100)
	//创建录制文件
	var flvFile *File
	if conf.ENABLE_RECORD {
		flvFile = createFlvFile(channel)
		log.Infof("Create record file path = /%s.flv\n", channel)
	}

	//创建rtp缓存队列
	var rtpQueue = newlistQueue(conf.RTP_CACHE_SIZE, SSRC)
	go rtpQueue.Run()
	go rtpQueue.printInfo()

	streamEntity := &StreamEntity{
		strLocalIdx: strLocalIdx,
		SSRC:        SSRC,
		channel:     channel,
		flvFile:     flvFile,
		RtpQueue:    rtpQueue,
		NextSeq:     RTP_INITIAL_SEQ,
	}

	ChannelMap.Put(SSRC, streamEntity)
	return streamEntity
}

type MyMessageHandler struct{}

// OnStreamCreated 自定义流创建方法
func (handler MyMessageHandler) OnStreamCreated(stream *rtmp.Stream) {
	ssrc := addChannel(stream.Channel()).SSRC
	stream.SetSsrc(ssrc)
	log.Infof("NewStreamCreated SSRC = %v\n", SSRC)
	streamNumberCount.Inc()

	if !running {
		RunSR("rtmp://127.0.0.1:1935/live/"+stream.Channel(), ssrc)
		running = true
	}
}

// OnStreamClosed 自定义流停止方法
func (handler MyMessageHandler) OnStreamClosed(stream *rtmp.Stream) {
	if val, ok := ChannelMap.Get(stream.Ssrc()); ok {
		streamEntity := val.(*StreamEntity)
		streamEntity.flvFile.Close()
		streamEntity.RtpQueue.Closed = true
	}
	ChannelMap.Remove(stream.Ssrc())
	streamNumberCount.Desc()
	log.Infof("StreamClosed SSRC = %v\n", stream.Ssrc())
}

func processKSR(readerFsr io.ReadCloser) *io.PipeReader {

	total_P, total_I, part_P := 1, 0, 0 //监测

	pr, pw := io.Pipe()
	rr, rw := io.Pipe()
	sr.TransToFlv(pr, rw)
	_, err := pw.Write(sr.HEADER_BYTES)
	checkError(err)

	go func() {
		var tmpBuf = make([]byte, 13) //去除头部字节
		_, err := io.ReadFull(readerFsr, tmpBuf)
		checkError(err)

		//flvFile_vsr, _ := CreateFile(outfile)

		for id := 0; ; id += 1 {
			headerFsr, dataFsr, _ := sr.ReadTag(readerFsr)

			sr.ParseHeader(headerFsr, dataFsr)
			vhFsr, _ := headerFsr.PktHeader.(sr.VideoPacketHeader)

			if headerFsr.TagType == byte(9) {

				if vh, ok := headerFsr.PktHeader.(sr.VideoPacketHeader); ok {
					checkError(err)

					if vh.IsSeq() {
						seqBytes = headerFsr.TagBytes

					} else if vh.IsKeyFrame() {
						keyTagBytes := sr.ReadKeyFrame(headerFsr.TagBytes, seqBytes)
						//sendFlvTag(keyTagBytes, streamEntity)
						_, err := pw.Write(keyTagBytes)
						checkError(err)
						err = binary.Write(pw, binary.BigEndian, uint32(len(keyTagBytes)))
						checkError(err)
						//err = flvFile_vsr.WriteTagDirect(keyTagBytes)
						//checkError(err)

						total_I += 1
						sr.Log.WithFields(logrus.Fields{
							"new_size":       len(keyTagBytes),
							"pre_size":       headerFsr.DataSize + 11,
							"is_KeyFrame":    vhFsr.IsKeyFrame(),
							"numBeforeLastI": part_P,
							"total I":        total_I,
							"I Proportion":   float32(total_I) / float32(total_P),
						}).Infof("instead keyFrame")
						part_P = 0
						continue
					} else {
						//是P帧
						total_P += 1
						part_P += 1
					}
				}
			}

			//err = flvFile_vsr.WriteTagDirect(headerFsr.TagBytes) //非IDR帧数据保持原有
			//sendFlvTag(headerFsr.TagBytes, streamEntity)
			_, err := pw.Write(headerFsr.TagBytes)
			checkError(err)
			err = binary.Write(pw, binary.BigEndian, uint32(len(headerFsr.TagBytes)))
			checkError(err)
			if vhFsr.IsKeyFrame() {
				sr.Log.WithFields(logrus.Fields{
					"size":      headerFsr.DataSize + 11,
					"timestamp": headerFsr.Timestamp,
				}).Warnf("ignore key frame")
			}
			checkError(err)

		}
	}()
	return rr
}

func readKSR(rr io.ReadCloser, ssrc uint32) {
	var streamEntity *StreamEntity
	if val, f := ChannelMap.Get(ssrc); f {
		streamEntity = val.(*StreamEntity)
	} else {
		panic("err sr")
	}

	//flvFile_vsr, _ := CreateFile(outfile)
	//pw.Write(HEADER_BYTES)

	go func() {
		var tmpBuf = make([]byte, 13) //去除头部字节
		_, err := io.ReadFull(rr, tmpBuf)
		checkError(err)

		for {
			headerFsr, _, _ := sr.ReadTag(rr)
			//time.Sleep(time.Second)
			//sr.ParseHeader(headerFsr, dataFsr)
			//vhFsr, _ := headerFsr.PktHeader.(sr.VideoPacketHeader)
			sendFlvTag(headerFsr.TagBytes, streamEntity)

			//if headerFsr.TagType == byte(9) {
			//
			//	if vh, ok := headerFsr.PktHeader.(sr.VideoPacketHeader); ok {
			//		if vh.IsKeyFrame() {
			//			fmt.Println(len(dataFsr))
			//		}
			//	}
			//}
		}
	}()
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
	//sendFlvTag(flvTag, streamEntity)

	//fmt.Println("rtp seq:", rp.Sequence(), ",payload size: ", len(metadata)+11, ",rtp timestamp: ", timestamp)
	//fmt.Println(flv_tag)
	if message.TimeStamp > 0 && s.StartTime == 0 { // 记录时间，最后一个timestamp为0的flvTag才是真正的startTime
		s.StartTime = time.Now().UnixMilli() - int64(message.TimeStamp)
	}

	if streamEntity.flvFile != nil { // 录制
		err := streamEntity.flvFile.WriteTagDirect(flvTag)
		checkError(err)
	}
}

func sendFlvTag(flvTag []byte, streamEntity *StreamEntity) {
	flv_tag_len := len(flvTag)
	if flv_tag_len <= MAX_RTP_PAYLOAD_LEN {
		rp := rsLocal.NewDataPacketForStream(streamEntity.strLocalIdx, streamEntity.timestamp)
		rp.SetMarker(true)
		rp.SetPayload(flvTag)
		rp.SetPayloadType(FLV_PAYLOAD_TYPE)
		rp.SetSequence(streamEntity.NextSeq) // 使用GoRtp自带的自增在多条流情况下出问题，所以手动设置
		streamEntity.NextSeq += 1
		sendPacket(rp)

		streamEntity.RtpQueue.packetQueue <- rp //入缓存
	} else {
		slice_num := int(math.Ceil(float64(flv_tag_len) / float64(MAX_RTP_PAYLOAD_LEN)))
		for i := 0; i < slice_num; i++ {
			rp := rsLocal.NewDataPacketForStream(streamEntity.strLocalIdx, streamEntity.timestamp)
			last_slice := i == slice_num-1
			rp.SetMarker(last_slice)
			if !last_slice {
				rp.SetPayload(flvTag[i*MAX_RTP_PAYLOAD_LEN : (i+1)*MAX_RTP_PAYLOAD_LEN])
			} else {
				rp.SetPayload(flvTag[i*MAX_RTP_PAYLOAD_LEN:])
			}
			rp.SetPayloadType(FLV_PAYLOAD_TYPE)
			rp.SetSequence(streamEntity.NextSeq)
			streamEntity.NextSeq += 1
			sendPacket(rp)

			streamEntity.RtpQueue.packetQueue <- rp //入缓存
		}
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
	if conf.DEBUG { // 单独调试模式
		return
	}
	for _, udpConn := range UdpConns.Values() {
		_, err := udpConn.(*net.UDPConn).Write(rp.Buffer()[:rp.InUse()])
		checkError(err)
	}
}

// 发送quic服务、http服务等端口初始化信息到边缘节点
func sendInitialMessage() {
	for _, udpConn := range UdpConns.Values() {
		var err error
		msg := new(bytes.Buffer)

		msg.WriteString("0001") // 标志位

		QuicPort, err := strconv.ParseInt(conf.QUIC_ADDR[1:], 10, 16)
		checkError(err)
		err = binary.Write(msg, binary.BigEndian, uint16(QuicPort))
		checkError(err)

		ApiPort, err := strconv.ParseInt(conf.API_ADDR[1:], 10, 16)
		checkError(err)
		err = binary.Write(msg, binary.BigEndian, uint16(ApiPort))
		checkError(err)

		_, err = udpConn.(*net.UDPConn).Write(msg.Bytes())
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
		opServer := api.NewServer(stream, conf.RTMP_ADDR)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error("HTTP-API server panic: ", r)
				}
			}()
			log.Info("HTTP-API listen On ", apiAddr)
			err := opServer.Serve(apiAddr, "", "") //不使用https
			checkError(err)
		}()
	}
}

func initLog() {

	//设置log
	log.Formatter = new(logrus.TextFormatter) //初始化log
	switch conf.LOG_LEVEL {
	case "debug":
		log.Level = logrus.DebugLevel
		break
	case "info":
		log.Level = logrus.InfoLevel
		break
	case "error":
		log.Level = logrus.ErrorLevel
		break
	default:
		log.Level = logrus.TraceLevel
	}
	log.Out = os.Stdout
	if conf.ENABLE_LOG_FILE {
		file, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			log.Out = file
		} else {
			log.Info("Failed to log to file, using default stderr")
		}
	}
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
	log.Info("quic server listening on ", "0.0.0.0"+conf.QUIC_ADDR)
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
        version: %s`, VERSION)

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
	initLog()

	initUdpConns()
	sendInitialMessage() //发送端口初始化信息到边缘，可去除
	initMetrics()        // grafana监控，可去除
	//go showRecvDataSize()

	go startQuic()
	startAPI(stream)  //提供流key与ssrc对应关系、流startTime等信息的api
	startRtmp(stream) //启动Rtmp服务器
}
