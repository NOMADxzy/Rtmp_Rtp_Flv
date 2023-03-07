package main

import (
	"fmt"
	"github.com/emirpasic/gods/maps/hashmap"
	"sync"
	"time"
)

//type rtpQueueItem struct {
//	packet *RTPPacket
//	seq    uint16
//}

//通过map实现的rtp缓存
type mapQueue struct {
	m            sync.RWMutex
	maxSize      int
	bytesInQueue int
	FirstSeq     uint16
	LastSeq      uint16
	RtpMap       *hashmap.Map
	totalSend    int
	totalLost    int
	Size         int
}

func newMapQueue(size int) *mapQueue {
	return &mapQueue{maxSize: size, RtpMap: hashmap.New()}
}

func (q *mapQueue) SizeOfNextRTP() int {
	q.m.RLock()
	defer q.m.RUnlock()

	if q.totalSend == 0 {
		return 0
	}
	val, found := q.RtpMap.Get(q.FirstSeq)
	if !found {
		return 0
	}
	return len(val.([]byte))
}

func (q *mapQueue) Clear() {
	q.m.Lock()
	defer q.m.Unlock()

	q.RtpMap.Clear()
	q.FirstSeq = 0
	q.LastSeq = 0
	q.totalSend = 0
	q.totalLost = 0
	q.bytesInQueue = 0
	q.Size = 0
}

func (q *mapQueue) Enqueue(pkt []byte, seq uint16) {
	q.m.Lock()
	defer q.m.Unlock()

	q.totalSend += 1
	q.Size += 1
	q.bytesInQueue += len(pkt)
	q.RtpMap.Put(seq, pkt)
	q.LastSeq = seq

	if q.Size > q.maxSize { //超出最大长度
		q.Size -= 1
		val, _ := q.RtpMap.Get(q.FirstSeq)
		q.RtpMap.Remove(q.FirstSeq)
		if q.FirstSeq == uint16(65535) {
			q.FirstSeq = uint16(0)
		} else {
			q.FirstSeq += uint16(1)
		}

		freed_size := len(val.([]byte))
		q.bytesInQueue -= freed_size
	} else if q.Size == 1 {
		q.FirstSeq = seq
	}
}

func (q *mapQueue) GetPkt(targetSeq uint16) []byte {
	q.m.RLock()
	defer q.m.RUnlock()

	q.totalLost += 1

	if val, f := q.RtpMap.Get(targetSeq); f {
		return val.([]byte)
	} else {
		return nil
	}

}

func (q *mapQueue) Check() bool {
	if q.FirstSeq < q.LastSeq {
		return int(q.LastSeq-q.FirstSeq+1) == q.Size
	} else if q.FirstSeq == q.LastSeq {
		return q.Size == 0 || q.Size == 1
	} else {
		return 65536-int(q.FirstSeq)+int(q.LastSeq)+1 == q.Size
	}
}

func (q *mapQueue) printInfo() {
	for {
		_ = <-time.After(5 * time.Second)
		fmt.Printf("current rtpQueue length: %d, FirstSeq: %d, LastSeq: %d, Packet_Loss_Rate:%.4f \n",
			q.Size, q.FirstSeq, q.LastSeq, float64(q.totalLost)/float64(q.totalSend))
		if !q.Check() {
			panic("rtp queue params err")
		}
	}
}
