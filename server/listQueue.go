package main

import (
	"fmt"
	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/prometheus/client_golang/prometheus"
	"net/rtp"
	"strconv"
	"sync"
	"time"
)

// 通过arraylist实现的rtp缓存
type listQueue struct {
	m               sync.RWMutex
	maxSize         int
	bytesInQueue    int
	FirstSeq        uint16
	LastSeq         uint16
	queue           *arraylist.List
	totalSend       int
	totalLost       int
	Closed          bool
	ssrc            uint32
	previousLostSeq uint16
	packetQueue     chan *rtp.DataPacket
}

func newlistQueue(size int, ssrc uint32) *listQueue {
	return &listQueue{queue: arraylist.New(), maxSize: size, ssrc: ssrc, packetQueue: make(chan *rtp.DataPacket, RTP_CACHE_CHAN_SIZE)}
}

func (q *listQueue) SizeOfNextRTP() int {
	q.m.RLock()
	defer q.m.RUnlock()

	if q.queue.Size() <= 0 {
		return 0
	}
	val, found := q.queue.Get(0)
	if !found {
		return 0
	}
	return len(val.([]byte))
}

func (q *listQueue) Clear() {
	q.m.Lock()
	defer q.m.Unlock()

	q.queue.Clear()
	q.bytesInQueue = 0
	q.FirstSeq = uint16(0)
	q.LastSeq = uint16(0)
	q.totalSend = 0
	q.totalLost = 0
	close(q.packetQueue)
}

func (q *listQueue) Run() {
	for {
		rp, ok := <-q.packetQueue
		if ok {
			rtpBuf := make([]byte, rp.InUse()) // 深拷贝：需要提前复制
			copy(rtpBuf, rp.Buffer()[:rp.InUse()])
			q.Enqueue(rtpBuf, rp.Sequence())
			rp.FreePacket() // 释放内存
		} else {
			fmt.Printf("[ssrc=%v]channel closed\n", q.ssrc)
			break
		}
	}
}

func (q *listQueue) Enqueue(pkt []byte, seq uint16) {
	q.m.Lock()
	defer q.m.Unlock()

	q.totalSend += 1
	q.bytesInQueue += len(pkt)
	streamCacheUsage.With(prometheus.Labels{"stream": strconv.Itoa(int(q.ssrc))}).Add(float64(len(pkt) / 1024))

	q.queue.Add(pkt)
	q.LastSeq = seq

	if q.queue.Size() > q.maxSize { // 超出最大长度
		val, _ := q.queue.Get(0)
		q.queue.Remove(0)
		q.FirstSeq += 1

		freeSize := len(val.([]byte))
		q.bytesInQueue -= freeSize
		streamCacheUsage.With(prometheus.Labels{"stream": strconv.Itoa(int(q.ssrc))}).Sub(float64(len(pkt) / 1024))
	} else if q.queue.Size() == 1 {
		q.FirstSeq = seq
	}
}

func (q *listQueue) GetPkt(targetSeq uint16) []byte {
	q.m.RLock()
	defer q.m.RUnlock()

	q.totalLost += 1
	if targetSeq+1 == q.previousLostSeq { //连续的三个seq丢失
		BusyTime += 1
		fmt.Printf("[warning] Continuous packet loss, -------------------- BusyTime  : %v \n", BusyTime)
	}
	q.previousLostSeq = targetSeq

	front := q.FirstSeq
	back := q.LastSeq

	if front < back { // 队列未循环
		if targetSeq < front || targetSeq > back {
			return nil
		} else {
			pkt, f := q.queue.Get(int(targetSeq - front))
			if f {
				return pkt.([]byte)
			}
		}
	} else { // 队列发生了循环
		if targetSeq >= front && targetSeq <= uint16(65535) {
			pkt, f := q.queue.Get(int(targetSeq - front))
			if f {
				return pkt.([]byte)
			}
		} else if targetSeq <= back {
			pkt, f := q.queue.Get(q.queue.Size() - 1 - int(back-targetSeq))
			if f {
				return pkt.([]byte)
			}
		}
	}
	return nil
}

func (q *listQueue) Check() bool {
	if q.FirstSeq < q.LastSeq {
		return int(q.LastSeq-q.FirstSeq+1) == q.queue.Size()
	} else if q.FirstSeq == q.LastSeq {
		return q.queue.Size() == 0 || q.queue.Size() == 1
	} else {
		return 65536-int(q.FirstSeq)+int(q.LastSeq)+1 == q.queue.Size()
	}
}

func (q *listQueue) printInfo() {
	for {
		_ = <-time.After(5 * time.Second)
		q.m.Lock()

		if q.Closed {
			return
		}
		fmt.Printf("[ssrc=%d]current rtpQueue length: %d, FirstSeq: %d, LastSeq: %d, Packet_Loss_Rate:%.4f \n",
			q.ssrc, q.queue.Size(), q.FirstSeq, q.LastSeq, float64(q.totalLost)/float64(q.totalSend))
		if !q.Check() {
			fmt.Printf("error in Rtp Cache, first:%v,last:%v, but Size:%v\n", q.FirstSeq, q.LastSeq, q.queue.Size())
			panic("rtp queue params err")
		}
		q.m.Unlock()
	}
}

func (q *listQueue) speedTest() {
	start := time.Now()
	for i := 0; i < q.queue.Size(); i++ {
		q.GetPkt(q.FirstSeq + uint16(i))
	}
	ts := time.Since(start).Nanoseconds()
	fmt.Printf("执行%d次查找，花费%dms", q.queue.Size(), ts)
}
