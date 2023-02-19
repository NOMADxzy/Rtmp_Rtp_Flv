package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/rtp"
	"os"
	"time"

	rtmp "github.com/zhangpeihao/gortmp"
	"github.com/zhangpeihao/log"
)

const (
	programName = "RtmpPlayer"
	version     = "0.0.1"
)

var (
	url        = flag.String("URL", RTMP_URL, "The rtmp url to connect.")
	streamName = flag.String("Stream", RTMP_STREAM, "Stream name to play.")
	dumpFlv    = flag.String("DumpFLV", "./recv.flv", "Dump FLV into file.")
)

type TestOutboundConnHandler struct {
}

var obConn rtmp.OutboundConn
var createStreamChan chan rtmp.OutboundStream
var videoDataSize int64
var audioDataSize int64
var flvFile *File
var status uint

func (handler *TestOutboundConnHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	status, err = conn.Status()
	fmt.Printf("@@@@@@@@@@@@@status: %d, err: %v\n", status, err)
}

func (handler *TestOutboundConnHandler) OnClosed(conn rtmp.Conn) {
	fmt.Printf("@@@@@@@@@@@@@Closed\n")
}

func (handler *TestOutboundConnHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
	//fmt.Println("--------------------------------")

	tagdata := message.Buf.Bytes()
	var flv_tag []byte
	timestamp := FLV_SEQ
	FLV_SEQ += uint32(1)

	switch message.Type {
	case rtmp.VIDEO_TYPE:
		//创建flv
		flv_tag = make([]byte, 11+len(tagdata))
		_, err := CreateTag(flv_tag, tagdata, VIDEO_TAG, message.AbsoluteTimestamp)
		if err != nil {
			panic(err)
		}
		if flvFile != nil {
			//flvFile.WriteVideoTag(message.Buf.Bytes(), message.AbsoluteTimestamp)
		}
		videoDataSize += int64(message.Buf.Len())
	case rtmp.AUDIO_TYPE:
		//创建flv
		flv_tag = make([]byte, 11+len(tagdata))
		_, err := CreateTag(flv_tag, tagdata, AUDIO_TAG, message.AbsoluteTimestamp)
		if err != nil {
			panic(err)
		}
		if flvFile != nil {
			//flvFile.WriteAudioTag(message.Buf.Bytes(), message.AbsoluteTimestamp)
		}
		audioDataSize += int64(message.Buf.Len())
	}
	//发送flv_tag，超长则分片发送
	flv_tag_len := len(flv_tag)
	var rp *rtp.DataPacket
	if flv_tag_len <= MAX_RTP_PAYLOAD_LEN {
		rp = rsLocal.NewDataPacket(uint32(timestamp))
		rp.SetMarker(true)
		rp.SetPayload(flv_tag)
		sendPacket(rp)

		rtp_buf := make([]byte, rp.InUse()) //复制一份放入map之中
		copy(rtp_buf, rp.Buffer()[:rp.InUse()])
		rtp_queue.Enqueue(rtp_buf, rp.Sequence())
		//fmt.Println(rtp_buf)
		//fmt.Println("当前rtp队列长度：", rtp_queue.queue.Len(), " 队列数据量：", rtp_queue.bytesInQueue)
		rp.FreePacket() //释放内存
	} else {
		slice_num := int(math.Ceil(float64(flv_tag_len) / float64(MAX_RTP_PAYLOAD_LEN)))
		for i := 0; i < slice_num; i++ {
			rp = rsLocal.NewDataPacket(uint32(timestamp))
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
			rtp_queue.Enqueue(rtp_buf, rp.Sequence())
			//fmt.Println("当前rtp队列长度：", rtp_queue.queue.Len(), " 队列数据量：", rtp_queue.bytesInQueue)
			rp.FreePacket() //释放内存
		}
	}

	//fmt.Println("rtp seq:", rp.Sequence(), ",payload size: ", len(tagdata)+11, ",rtp timestamp: ", timestamp)
	//fmt.Println(flv_tag)
	err := flvFile.WriteTagDirect(flv_tag)
	if err != nil {
		return
	}

}

func (handler *TestOutboundConnHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	fmt.Printf("ReceviedCommand: %+v\n", command)
}

func (handler *TestOutboundConnHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	fmt.Printf("Stream created: %d\n", stream.ID())
	createStreamChan <- stream
}

func sendPacket(rp *rtp.DataPacket) {
	if USE_MULTICAST { //组播
		sendPacketmulticast(rp)
	} else { //单播
		r := rand.Intn(100)
		if float64(r)/100.0 >= PACKET_LOSS_RATE {
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
	r := rand.Intn(100)
	if float64(r)/100.0 >= PACKET_LOSS_RATE {
		_, err := udpConn.Write(rp.Buffer()[:rp.InUse()])
		if err != nil {
			return
		}
	}
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	fmt.Printf("rtmp:%s stream:%s flv:%s\r\n", *url, *streamName, *dumpFlv)
	l := log.NewLogger(".", "player", nil, 60, 3600*24, true)
	rtmp.InitLogger(l)
	defer l.Close()

	//rtpsession初始化
	tpLocal, _ := rtp.NewTransportUDP(local, localPort, localZone)
	rsLocal = rtp.NewSession(tpLocal, tpLocal)
	rsLocal.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1, remoteZone})
	strLocalIdx, _ := rsLocal.NewSsrcStreamOut(&rtp.Address{local.IP, localPort, localPort + 1, localZone}, 1020304, CUR_SEQ)
	rsLocal.SsrcStreamOutForIndex(strLocalIdx).SetPayloadType(9)
	rsLocal.StartSession()
	defer rsLocal.CloseSession()

	// Create flv file
	if len(*dumpFlv) > 0 {
		var err error
		flvFile, err = CreateFile(*dumpFlv)
		if err != nil {
			fmt.Println("Create FLV dump file error:", err)
			return
		}
	}
	defer func() {
		if flvFile != nil {
			flvFile.Close()
		}
	}()

	createStreamChan = make(chan rtmp.OutboundStream)
	testHandler := &TestOutboundConnHandler{}
	fmt.Println("to dial")
	var err error
	obConn, err = rtmp.Dial(*url, testHandler, 100)
	/*
		conn := TryHandshakeByVLC()
		obConn, err = rtmp.NewOutbounConn(conn, *url, testHandler, 100)
	*/
	if err != nil {
		fmt.Println("Dial error", err)
		os.Exit(-1)
	}

	defer obConn.Close()
	fmt.Printf("obConn: %+v\n", obConn)
	fmt.Printf("obConn.URL(): %s\n", obConn.URL())
	fmt.Println("to connect")
	//	err = obConn.Connect("33abf6e996f80e888b33ef0ea3a32bfd", "131228035", "161114738", "play", "", "", "1368083579")
	err = obConn.Connect()
	if err != nil {
		fmt.Printf("Connect error: %s", err.Error())
		os.Exit(-1)
	}

	go func() {
		fmt.Println("quic协程启动")
		conn := initialQUIC()
		var seq uint16
		for {
			//fmt.Println("quic线程启动，等待重传序列号")

			_, err = conn.ReadSeq(&seq)
			if err != nil {
				//避免长时间收不到重传请求导致quic停止
				time.Sleep(time.Second)
				continue
			}
			fmt.Println("收到重传请求，seq: ", seq)

			//发送rtp数据包给客户
			pkt := rtp_queue.GetPkt(seq)
			//fmt.Println(pkt)
			if pkt != nil {
				_, err = conn.SendRtp(pkt)
				if err != nil {
					panic(err)
				}
			}
		}
	}()

	for {
		select {
		case stream := <-createStreamChan:
			// Play
			err = stream.Play(*streamName, nil, nil, nil)
			if err != nil {
				fmt.Printf("Play error: %s", err.Error())
				os.Exit(-1)
			}
			// Set Buffer Length

		case <-time.After(5 * time.Second):
			fmt.Printf("Audio size: %d bytes; Vedio size: %d bytes\n", audioDataSize, videoDataSize)
		}
	}
}

////////////////////////////////////////////
func CheckC1(c1 []byte, offset1 bool) (uint32, error) {
	var clientDigestOffset uint32
	if offset1 {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 8, 728, 12)
	} else {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 772, 728, 776)
	}
	// Create temp buffer
	tmpBuf := new(bytes.Buffer)
	tmpBuf.Write(c1[:clientDigestOffset])
	tmpBuf.Write(c1[clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH:])
	// Generate the hash
	tempHash, err := rtmp.HMACsha256(tmpBuf.Bytes(), rtmp.GENUINE_FP_KEY[:30])
	if err != nil {
		return 0, errors.New(fmt.Sprintf("HMACsha256 err: %s\n", err.Error()))
	}
	expect := c1[clientDigestOffset : clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH]
	if bytes.Compare(expect, tempHash) != 0 {
		return 0, errors.New(fmt.Sprintf("C1\nExpect % 2x\nGot    % 2x\n",
			expect,
			tempHash))
	}
	return clientDigestOffset, nil
}
