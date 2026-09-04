package team

import "sync"

type Message struct {
	Sender    string
	Recipient string
	TaskID    string
	Sequence  uint64
	TraceID   string
	Content   string
}

type Mailbox struct {
	mu       sync.Mutex
	nextSeq  uint64
	messages []Message
}

func (m *Mailbox) Send(msg Message) Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeq++
	msg.Sequence = m.nextSeq
	m.messages = append(m.messages, msg)
	return msg
}

func (m *Mailbox) List() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.messages...)
}
