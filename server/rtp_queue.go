package main

import (
	"container/list"
	"github.com/emirpasic/gods/maps/hashmap"
	"sync"
)

//type rtpQueueItem struct {
//	packet *RTPPacket
//	seq    uint16
//}

type queue struct {
	m            sync.RWMutex
	maxSize      int
	bytesInQueue int
	queue        *list.List
	RtpMap       *hashmap.Map
}

func newQueue(size int) *queue {
	return &queue{queue: list.New(), maxSize: size, RtpMap: hashmap.New()}
}

func (q *queue) SizeOfNextRTP() int {
	q.m.RLock()
	defer q.m.RUnlock()

	if q.queue.Len() <= 0 {
		return 0
	}
	val, found := q.RtpMap.Get(q.queue.Front().Value.(uint16))
	if !found {
		return 0
	}
	return len(val.([]byte))
}

func (q *queue) SeqNrOfNextRTP() uint16 {
	q.m.RLock()
	defer q.m.RUnlock()

	if q.queue.Len() <= 0 {
		return 0
	}

	return q.queue.Front().Value.(uint16)
}

func (q *queue) SeqNrOfLastRTP() uint16 {
	q.m.RLock()
	defer q.m.RUnlock()

	if q.queue.Len() <= 0 {
		return 0
	}

	return q.queue.Back().Value.(uint16)
}

//func (q *queue) BytesInQueue() int {
//	q.m.Lock()
//	defer q.m.Unlock()
//
//	return q.bytesInQueue
//}

func (q *queue) SizeOfQueue() int {
	q.m.RLock()
	defer q.m.RUnlock()

	return q.queue.Len()
}

func (q *queue) Clear() int {
	q.m.Lock()
	defer q.m.Unlock()

	size := q.queue.Len()
	front := q.queue.Front()
	for front != nil {
		q.RtpMap.Remove(front.Value)
		front = front.Next()
	}
	q.bytesInQueue = 0
	q.queue.Init()
	return size
}

func (q *queue) Enqueue(pkt []byte, seq uint16) {
	q.m.Lock()
	defer q.m.Unlock()

	q.bytesInQueue += len(pkt)
	q.queue.PushBack(seq)
	q.RtpMap.Put(seq, pkt)
	if q.queue.Len() > q.maxSize { //超出最大长度
		front := q.queue.Front()
		q.queue.Remove(front)
		val, _ := q.RtpMap.Get(front.Value)
		freed_size := len(val.([]byte))
		q.RtpMap.Remove(front.Value)
		q.bytesInQueue -= freed_size
	}
}

func (q *queue) Dequeue() interface{} {
	q.m.Lock()
	defer q.m.Unlock()

	if q.queue.Len() <= 0 {
		return nil
	}

	front := q.queue.Front()
	q.queue.Remove(front)
	packet, _ := q.RtpMap.Get(front.Value)
	q.RtpMap.Remove(front.Value)
	q.bytesInQueue -= len(packet.([]byte))
	return packet
}

func (q *queue) GetPkt(targetSeq uint16) []byte {
	front := q.queue.Front().Value.(uint16)
	back := q.queue.Back().Value.(uint16)
	if targetSeq < front || targetSeq > back { //不在队列中
		return nil
	} else {
		val, f := q.RtpMap.Get(targetSeq)
		if f {
			return val.([]byte)
		}
		return nil
	}
}
