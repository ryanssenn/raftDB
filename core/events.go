package core

import (
	"sync"
	"time"
)

const eventCapacity = 200

type Event struct {
	Seq     int64  `json:"seq"`
	Ts      int64  `json:"ts"`
	Type    string `json:"type"`
	From    string `json:"from"`
	To      string `json:"to"`
	Term    int64  `json:"term,omitempty"`
	Entries int    `json:"entries,omitempty"`
	Op      string `json:"op,omitempty"`
	Key     string `json:"key,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type EventLog struct {
	mu     sync.Mutex
	events []Event
	seq    int64
	start  int
	count  int
}

func NewEventLog() *EventLog {
	return &EventLog{
		events: make([]Event, eventCapacity),
	}
}

func (l *EventLog) Record(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.seq++
	e.Seq = l.seq
	if e.Ts == 0 {
		e.Ts = time.Now().UnixMilli()
	}

	if l.count < eventCapacity {
		idx := (l.start + l.count) % eventCapacity
		l.events[idx] = e
		l.count++
	} else {
		l.events[l.start] = e
		l.start = (l.start + 1) % eventCapacity
	}

	return e
}

func (l *EventLog) Since(since int64) ([]Event, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []Event
	for i := 0; i < l.count; i++ {
		e := l.events[(l.start+i)%eventCapacity]
		if e.Seq > since {
			out = append(out, e)
		}
	}
	return out, l.seq
}

func (n *Node) recordEvent(e Event) {
	if n.Events == nil {
		return
	}
	n.Events.Record(e)
}
