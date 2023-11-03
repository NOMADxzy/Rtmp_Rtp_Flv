package main

import (
	"github.com/sirupsen/logrus"
	"io"
	"rtmp_rtp_flv/sr"
	"strconv"
)

func RunSR(inFile string, ssrc uint32) {

	//var err error
	sr.InitLog()
	w, h := sr.GetVideoSize(inFile)
	scale := 4
	sr.Conf = &sr.Config{w, h, scale, w * scale, h * scale,
		conf.SR_API}
	sr.Log.WithFields(logrus.Fields{
		"video source:": inFile,
		"video size":    strconv.Itoa(w) + "x" + strconv.Itoa(h),
	}).Infof("RunSR of KeyFrame")

	pr, pw := io.Pipe()
	sr.InitKeyProcess() //对超分后的图片进行h264编码的服务

	_ = sr.FSR(inFile, pw) // ffmpeg 扩大分辨率

	//_, fileName := filepath.Split(inFile)
	//rawName := strings.Split(fileName, ".")[0]
	rr := processKSR(pr) // pr -> 替换关键帧、转码 -> rr

	readKSR(rr, ssrc) // rr -> 读取转码后的视频的Tag并发送

}
