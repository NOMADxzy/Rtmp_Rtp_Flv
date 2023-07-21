package sr

import (
	"bytes"
	"encoding/binary"
	"fmt"
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"io"
	"io/ioutil"
	"net/http"
)

type Config struct {
	Ow    int    //原视频宽
	Oh    int    //原视频高
	Scale int    //超分倍数，默认4
	W     int    //新视频宽
	H     int    //新视频高
	SrApi string // sr后端服务地址
}

var Conf *Config

func GetVideoSize(fileName string) (int, int) {
	//return 160, 90
	return 320, 180
	//Log.Infof("Getting video size for", fileName)
	//data, err := ffmpeg.Probe(fileName)
	//if err != nil {
	//	panic(err)
	//}
	////log.Println("got video info", data)
	//type VideoInfo struct {
	//	Streams []struct {
	//		CodecType string `json:"codec_type"`
	//		Width     int
	//		Height    int
	//	} `json:"streams"`
	//}
	//vInfo := &VideoInfo{}
	//err = json.Unmarshal([]byte(data), vInfo)
	//if err != nil {
	//	panic(err)
	//}
	//for _, s := range vInfo.Streams {
	//	if s.CodecType == "video" {
	//		return s.Width, s.Height
	//	}
	//}
	//return 0, 0
}

func TransToFlv(reader io.ReadCloser, writer io.WriteCloser) <-chan error {
	Log.Infof("Starting transToFlv")
	done := make(chan error)
	go func() {
		err := ffmpeg.Input("pipe:").
			Output("pipe:",
				ffmpeg.KwArgs{
					"vcodec": "copy", "format": "flv", "pix_fmt": "yuv420p",
				}).
			WithOutput(writer).
			WithInput(reader).
			Run()
		Log.Warnf("transToFlv done")
		_ = writer.Close()
		done <- err
		close(done)
	}()
	return done
}

func FSR(infileName string, writer io.WriteCloser) <-chan error {
	Log.Infof("Starting ffmpeg sr")
	done := make(chan error)
	go func() {
		err := ffmpeg.Input(infileName).
			Output("pipe:",
				ffmpeg.KwArgs{
					"s": fmt.Sprintf("%dx%d", Conf.W, Conf.H), "format": "flv", "vcodec": "libx264",
				}).
			WithOutput(writer).
			Run()
		Log.Warnf("ffmpeg sr done")
		//_ = writer.Close()
		done <- err
		close(done)
	}()

	return done
}

var buf *bytes.Buffer

func clipPreKeyframe(reader io.Reader) chan error {

	buf = bytes.NewBuffer(nil)
	done := make(chan error)
	go func() {
		err := ffmpeg.Input("pipe:",
			ffmpeg.KwArgs{"format": "flv"}).
			Output("pipe:", ffmpeg.KwArgs{"format": "rawvideo", "s": fmt.Sprintf("%dx%d", Conf.Ow, Conf.Oh), "pix_fmt": "rgb24"}).
			WithInput(reader).
			WithOutput(buf).
			Run()
		done <- err
		close(done)
	}()
	return done
}

func ReadKeyFrame(keyframeBytes []byte, seqBytes []byte) []byte {
	Log.Debugf("Starting read KeyFrame")

	tmpBuf := bytes.NewBuffer(HEADER_BYTES)
	tmpBuf.Write(seqBytes)
	err := binary.Write(tmpBuf, binary.BigEndian, uint32(len(seqBytes)))
	CheckErr(err)
	tmpBuf.Write(keyframeBytes)
	err = binary.Write(tmpBuf, binary.BigEndian, uint32(len(keyframeBytes)))
	CheckErr(err)

	done := clipPreKeyframe(bytes.NewReader(tmpBuf.Bytes()))
	<-done
	a := buf.Bytes()
	fmt.Println(len(a))
	body := PostImg(buf.Bytes())

	encToH264(body) //会在keyChan中产生相应的超分tag
	return <-keyChan

}

func ParseHeader(header *TagHeader, data []byte) {
	var tag Tag
	_, err := tag.ParseMediaTagHeader(data, header.TagType == byte(9))
	CheckErr(err)
	header.PktHeader = &tag
}

func PostImg(bytesData []byte) []byte {

	request, err := http.NewRequest("POST", fmt.Sprintf("%s?w=%d&h=%d", Conf.SrApi, Conf.Ow, Conf.Oh), bytes.NewBuffer(bytesData))
	CheckErr(err)
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")

	client := http.Client{}
	resp, err := client.Do(request)
	CheckErr(err)

	body, err := ioutil.ReadAll(resp.Body)
	CheckErr(err)

	return body
}
