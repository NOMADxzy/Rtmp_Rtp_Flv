package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/quic-go/quic-go"
	"math/big"
	"net"
	"os"
	"time"
)

func WaitForQuicConn(ln quic.Listener) *Conn {
	protoConn, err := ln.Accept(context.Background())
	conn, _ := newConn(protoConn, true)
	checkError(err)

	log.Infof("Quic Established connection, remote addr: %v\n", protoConn.RemoteAddr())
	return conn
}

func createFlvFile(channel string) *File {
	flvFile, err := CreateFile("./" + channel + ".flv")
	if err != nil {
		log.Error("Create FLV dump file error:", err)
		panic(err)
	}
	return flvFile
}

func NewUDPConn(address string) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

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
		log.Error(err)
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
