package main

import (
	"fmt"
	"github.com/emirpasic/gods/lists/arraylist"
	"sync"
	"time"
)

//通过arraylist实现的rtp缓存
type listQueue struct {
	m            sync.RWMutex
	maxSize      int
	bytesInQueue int
	FirstSeq     uint16
	LastSeq      uint16
	queue        *arraylist.List
	totalSend    int
	totalLost    int
	Closed       bool
}

func newlistQueue(size int) *listQueue {
	return &listQueue{queue: arraylist.New(), maxSize: size}
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
}

func (q *listQueue) Enqueue(pkt []byte, seq uint16) {
	q.m.Lock()
	defer q.m.Unlock()

	q.totalSend += 1
	q.bytesInQueue += len(pkt)
	q.queue.Add(pkt)
	q.LastSeq = seq

	if q.queue.Size() > q.maxSize { //超出最大长度
		val, _ := q.queue.Get(0)
		q.queue.Remove(0)
		if q.FirstSeq == uint16(65535) {
			q.FirstSeq = 0
		} else {
			q.FirstSeq += 1
		}

		freed_size := len(val.([]byte))
		q.bytesInQueue -= freed_size
	} else if q.queue.Size() == 1 {
		q.FirstSeq = seq
	}
}

func (q *listQueue) GetPkt(targetSeq uint16) []byte {
	q.m.RLock()
	defer q.m.RUnlock()

	q.totalLost += 1
	front := q.FirstSeq
	back := q.LastSeq

	if front < back { //队列未循环
		if targetSeq < front || targetSeq > back {
			return nil
		} else {
			pkt, f := q.queue.Get(int(targetSeq - front))
			if f {
				return pkt.([]byte)
			}
		}
	} else { //队列发生了循环
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
		if q.Closed {
			return
		}
		fmt.Printf("current rtpQueue length: %d, FirstSeq: %d, LastSeq: %d, Packet_Loss_Rate:%.4f \n",
			q.queue.Size(), q.FirstSeq, q.LastSeq, float64(q.totalLost)/float64(q.totalSend))
		if !q.Check() {
			panic("rtp queue params err")
		}
	}
}
