package main

import (
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net"
	"net/rtp"
	_ "strconv"
)

var (
	HEADER_BYTES = []byte{'F', 'L', 'V', 0x01, 0x05, 0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x00,
		0x12, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 11
		0x02, 0x00, 0x0a, 0x6f, 0x6e, 0x4d, 0x65, 0x74, 0x61, 0x44, 0x61, 0x74, 0x61, // 13
		0x08, 0x00, 0x00, 0x00, 0x01, // 5
		0x00, 0x08, 0x64, 0x75, 0x72, 0x61, 0x74, 0x69, 0x6F, 0x6E, // 10
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 9
		0x00, 0x00, 0x09, // 3
		0x00, 0x00, 0x00, 0x33}
)

// 常量
const (
	AUDIO_TAG           = byte(0x08)
	VIDEO_TAG           = byte(0x09)
	SCRIPT_DATA_TAG     = byte(0x12)
	DURATION_OFFSET     = 53
	HEADER_LEN          = 13
	MAX_RTP_PAYLOAD_LEN = 1180
	RTP_INITIAL_SEQ     = uint16(65000)
	RTP_CACHE_CHAN_SIZE = 100
	FLV_PAYLOAD_TYPE    = byte(100)
)

// rtp相关
var local, _ = net.ResolveIPAddr("ip", "127.0.0.1")
var rsLocal *rtp.Session
var localZone = ""
var BusyTime = 0 //记录边缘拥塞次数

var log = logrus.New()

// Config app参数
type Config struct {
	RTP_CACHE_SIZE   int      `yaml:"rtp_cache_size"`
	QUIC_ADDR        string   `yaml:"quic_addr"`
	CLIENT_ADDR_LIST []string `yaml:"client_addr_list"`
	ENABLE_RECORD    bool     `yaml:"enable_record"`
	RTP_PORT         int      `yaml:"rtp_port"`
	RTMP_ADDR        string   `yaml:"rtmp_addr"`
	API_ADDR         string   `yaml:"api_addr"`
	DEBUG            bool     `yaml:"debug"`
	LOG_LEVEL        string   `yaml:"log_level"`
	ENABLE_LOG_FILE  bool     `yaml:"enable_log_file"`
}

var conf = &Config{ //default config
	RTP_CACHE_SIZE:   5000,
	QUIC_ADDR:        ":4242",
	CLIENT_ADDR_LIST: []string{"239.0.0.1:5222"},
	ENABLE_RECORD:    false,
	RTP_PORT:         5220,
	RTMP_ADDR:        ":1935",
	API_ADDR:         ":8090",
	DEBUG:            false,
	LOG_LEVEL:        "debug",
	ENABLE_LOG_FILE:  false,
}

func (conf *Config) readFromXml(src string) {
	content, err := ioutil.ReadFile(src)
	if err != nil {
		conf.writeToXml(src)
		return
	}
	err = yaml.Unmarshal(content, conf)
	checkError(err)
}
func (conf *Config) writeToXml(src string) {
	data, err := yaml.Marshal(conf)
	checkError(err)
	err = ioutil.WriteFile(src, data, 0777)
	checkError(err)
}
