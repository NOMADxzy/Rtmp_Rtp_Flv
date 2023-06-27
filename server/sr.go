package main

import (
	"fmt"
	"io"
	"path/filepath"
	"rtmp_rtp_flv/sr"
	"strings"
)

func RunSR(inFile string, ssrc uint32) {
	fmt.Println("inFile: " + inFile)

	//var err error
	sr.InitLog()
	w, h := sr.GetVideoSize(inFile)
	scale := 4
	sr.Conf = &sr.Config{w, h, scale, w * scale, h * scale,
		"http://10.112.90.187:5000/"}
	//"http://127.0.0.1:5000/"}
	log.Println(w, h)

	pr1, pw1 := io.Pipe()
	pr2, pw2 := io.Pipe()
	sr.InitKeyProcess() //对超分后的图片进行h264编码的服务

	_ = sr.TransToFlv(inFile, pw1) // 转码为flv
	_ = sr.FSR(inFile, pw2)

	_, fileName := filepath.Split(inFile)
	rawName := strings.Split(fileName, ".")[0]
	processKSR(pr1, pr2, "out/"+rawName+".flv", ssrc) // 提取关键帧

}
