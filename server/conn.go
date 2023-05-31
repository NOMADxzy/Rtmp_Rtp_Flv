package main

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/quic-go/quic-go"
	"github.com/sirupsen/logrus"
)

type Conn struct {
	Connection quic.Connection
	infoStream quic.Stream
	dataStream quic.Stream
}

// 自定义的Conn，方便操作
func newConn(sess quic.Connection, is_server bool) (*Conn, error) {
	quicStream, err := sess.OpenStream()

	if err != nil {
		return nil, err
	}
	return &Conn{
		Connection: sess,
		dataStream: quicStream,
	}, nil
}

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
	log.Info("[quic] timeout, conn closed")
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
		log.WithFields(logrus.Fields{
			"seq":  seq,
			"ssrc": ssrc,
		}).Debugf("retransmission req recieved")

		//发送rtp数据包给客户
		val, f := ChannelMap.Get(ssrc)
		if !f {
			log.Errorf("get streamInfo faild, ssrc = %d", ssrc)
			continue
		}
		pkt := val.(*StreamEntity).RtpQueue.GetPkt(seq)
		//fmt.Println(pkt)
		if pkt != nil {
			_, err = c.SendRtp(pkt)
			checkError(err)
		} else {
			log.Errorf("[quic] retransmission failed，no such pkt，seq：%v", seq)
			_, err := c.SendRtp(nil)
			checkError(err)
		}
	}
}
