package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"calor/api/internal/store"
)

var ErrQueueFull = errors.New("ingest queue full")

type queueItem struct {
	events []store.Event
	done   chan struct{} // set for flush markers
}

// Queue decouples ingest requests from SQLite writes: a single writer
// goroutine coalesces many small batches into one transaction, which keeps
// fsyncs and lock contention low on slow disks. Capacity is bounded; when
// full, ingest answers 503 instead of growing memory.
type Queue struct {
	st       *store.Store
	ch       chan queueItem
	interval time.Duration
	log      *log.Logger
	wg       sync.WaitGroup
	once     sync.Once
}

const maxTxEvents = 5000

func NewQueue(st *store.Store, capacity int, interval time.Duration, l *log.Logger) *Queue {
	q := &Queue{st: st, ch: make(chan queueItem, capacity), interval: interval, log: l}
	q.wg.Add(1)
	go q.run()
	return q
}

func (q *Queue) Len() int { return len(q.ch) }

func (q *Queue) Enqueue(events []store.Event) error {
	select {
	case q.ch <- queueItem{events: events}:
		return nil
	default:
		return ErrQueueFull
	}
}

// Flush waits until everything enqueued before the call is committed.
func (q *Queue) Flush() {
	done := make(chan struct{})
	q.ch <- queueItem{done: done}
	<-done
}

func (q *Queue) Close() {
	q.once.Do(func() {
		close(q.ch)
		q.wg.Wait()
	})
}

func (q *Queue) run() {
	defer q.wg.Done()
	buf := make([]store.Event, 0, 512)
	write := func() {
		if len(buf) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := q.st.InsertEvents(ctx, buf); err != nil {
			q.log.Printf("ingest write failed (%d events dropped): %v", len(buf), err)
		}
		cancel()
		clear(buf)
		buf = buf[:0]
	}
	timer := time.NewTimer(q.interval)
	defer timer.Stop()
	for {
		select {
		case it, ok := <-q.ch:
			if !ok {
				write()
				return
			}
			buf = append(buf, it.events...)
			if it.done != nil {
				write()
				close(it.done)
				continue
			}
			if len(buf) >= maxTxEvents {
				write()
			}
		case <-timer.C:
			write()
			timer.Reset(q.interval)
		}
	}
}
