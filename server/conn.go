package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/quic-go/quic-go"
)

type Conn struct {
	Connection quic.Connection
	infoStream quic.Stream
	dataStream quic.Stream
}

// 自定义的Conn，方便操作
func newConn(sess quic.Connection, is_server bool) (*Conn, error) {
	quicStream, err := sess.OpenStream()
	if is_server {
		fmt.Print("This is quic server launched by cloudServer.\n")
	}
	if err != nil {
		return nil, err
	}
	return &Conn{
		Connection: sess,
		dataStream: quicStream,
	}, nil
}

//	func (c *conn) DataStream() quic.Stream {
//		return c.dataStream
//	}

func (c *Conn) ReadSeq(seq *uint16) (int, error) {
	if c.infoStream == nil {
		var err error
		c.infoStream, err = c.Connection.AcceptStream(context.Background())
		// TODO: check stream id
		if err != nil {
			return 0, err
		}
	}
	seq_b := make([]byte, 2)
	_, err := c.infoStream.Read(seq_b)
	if err != nil {
		return -1, err
	}
	*seq = binary.BigEndian.Uint16(seq_b)
	return 0, err
}

func (c *Conn) ReadSsrc(ssrc *uint32) error {
	if c.infoStream == nil {
		var err error
		c.infoStream, err = c.Connection.AcceptStream(context.Background())
		// TODO: check stream id
		if err != nil {
			return err
		}
	}
	ssrc_b := make([]byte, 4)
	_, err := c.infoStream.Read(ssrc_b)
	if err != nil {
		return errors.New("read ssrc failed")
	}
	*ssrc = binary.BigEndian.Uint32(ssrc_b)
	return err
}

func (c *Conn) SendLen(len uint16) (int, error) {
	len_b := make([]byte, 2)
	binary.BigEndian.PutUint16(len_b, len)
	return c.infoStream.Write(len_b)
}

func (c *Conn) SendRtp(pkt []byte) (int, error) {
	if pkt == nil { // 缓存中没找到该包
		_, err := c.SendLen(uint16(0))
		if err != nil {
			panic(err)
		}
		return 0, nil
	} else {
		_, err := c.SendLen(uint16(len(pkt)))
		if err != nil {
			panic(err)
		}
		return c.dataStream.Write(pkt)
	}
}

func (c *Conn) Close() {
	err := c.infoStream.Close()
	checkError(err)
	err = c.dataStream.Close()
	checkError(err)
	c = nil
	fmt.Println("timeout conn closed")
}

func (c *Conn) Serve() {
	// 通过ssrc和seq找到所需的rtp包
	var seq uint16
	var ssrc uint32

	// quic线程启动，等待重传序列号
	for {
		err := c.ReadSsrc(&ssrc)
		if err != nil {
			// 长时间收不到重传请求会触发err
			c.Close()
			return
		}

		_, err = c.ReadSeq(&seq)
		checkError(err)
		fmt.Println("收到重传请求，seq: ", seq)

		// 发送rtp数据包给客户
		val, f := ChannelMap.Get(ssrc)
		if !f {
			fmt.Printf("error,can not find streamInfo, ssrc = %d\n", ssrc)
			continue
		}
		pkt := val.(*StreamInfo).RtpQueue.GetPkt(seq)
		// fmt.Println(pkt)
		if pkt != nil {
			_, err = c.SendRtp(pkt)
			checkError(err)
		} else {
			fmt.Println("quic无法重传，没有该包，seq：", seq)
			_, err := c.SendRtp(nil)
			checkError(err)
		}
	}
}
