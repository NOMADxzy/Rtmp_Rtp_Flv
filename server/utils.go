package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/lucas-clemente/quic-go"
	//quicconn "github.com/marten-seemann/quic-conn"

	"math/big"
	"time"
)

func initialQUIC() *conn {
	tlsConf, err := generateTLSConfig()
	if err != nil {
		panic(err)
	}

	ln, err := quic.ListenAddr("localhost:4242", tlsConf, nil)

	if err != nil {
		panic(err)
	}

	fmt.Println("Waiting for incoming connection")
	protoconn, err := ln.Accept(context.Background())

	conn, _ := newConn(protoconn, true)

	if err != nil {
		panic(err)
	}
	fmt.Println("Established connection")
	return conn
}

var rtp_queue = newQueue(50000)
var CUR_SEQ = uint16(4171)
var FLV_SEQ = uint32(0)

//func main() {
//	// start the server
//	go func() {
//
//
//		var seq uint16
//
//		_, err = conn.ReadSeq(&seq)
//		if err != nil {
//			panic(err)
//		}
//		//msg := make([]byte, msg_len+10)
//		//
//		//_, err = conn.Read(msg)
//		fmt.Println("seq: ", seq)
//
//		//发送rtp数据包给客户
//
//		//rtp := []byte("Rtp data...")
//		payload := make([]byte, 16)
//		for i := range payload {
//			payload[i] = byte(i)
//		}
//
//		rtp_queue := newQueue(10)
//		for i := uint16(0); i < uint16(12); i++ {
//			new_pkt := NewRTPPacket(payload, int8(2), uint16(1), uint32(2), uint32(3))
//			rtp_queue.Enqueue(new_pkt, seq+i-uint16(3))
//		}
//
//		pkt := rtp_queue.GetPkt(seq)
//		_, err = conn.SendRtp(*pkt)
//		if err != nil {
//			panic(err)
//		}
//	}()
//
//	time.Sleep(time.Hour)
//}

func generateTLSConfig() (*tls.Config, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	b := pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	certPEM := pem.EncodeToMemory(&b)

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-server"},
	}, nil
}
