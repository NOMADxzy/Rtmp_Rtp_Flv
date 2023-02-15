package main

import (
	"context"
	"encoding/binary"
	"github.com/lucas-clemente/quic-go"
)

type conn struct {
	session    quic.Session
	infoStream quic.Stream
	dataStream quic.Stream
}

//自定义的Conn，方便操作
func newConn(sess quic.Session, is_server bool) (*conn, error) {
	if is_server {
		dstream, err := sess.OpenStream()
		if err != nil {
			return nil, err
		}
		return &conn{
			session:    sess,
			dataStream: dstream,
		}, nil
	} else {
		istream, err := sess.OpenStream()
		if err != nil {
			return nil, err
		}
		return &conn{
			session:    sess,
			infoStream: istream,
		}, nil
	}
}

//func (c *conn) DataStream() quic.Stream {
//	return c.dataStream
//}
func (c *conn) ReadSeq(seq *uint16) (int, error) {
	if c.infoStream == nil {
		var err error
		c.infoStream, err = c.session.AcceptStream(context.Background())
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

	//return io.ReadFull(c.dataStream,b)
}
func (c *conn) SendLen(obj []byte) (int, error) {
	len_b := make([]byte, 2)
	binary.BigEndian.PutUint16(len_b, uint16(len(obj)))
	return c.infoStream.Write(len_b)
}

func (c *conn) SendRtp(pkt []byte) (int, error) {
	_, err := c.SendLen(pkt)
	if err != nil {
		panic(err)
	}
	return c.dataStream.Write(pkt)
}
