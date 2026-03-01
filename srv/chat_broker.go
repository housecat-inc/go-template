package srv

import (
	"sync"
)

type ChatBroker struct {
	mu          sync.RWMutex
	subscribers map[int64]map[chan []byte]struct{}
}

func NewChatBroker() *ChatBroker {
	return &ChatBroker{
		subscribers: make(map[int64]map[chan []byte]struct{}),
	}
}

func (b *ChatBroker) Subscribe(chatID int64) chan []byte {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	if b.subscribers[chatID] == nil {
		b.subscribers[chatID] = make(map[chan []byte]struct{})
	}
	b.subscribers[chatID][ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *ChatBroker) Unsubscribe(chatID int64, ch chan []byte) {
	b.mu.Lock()
	delete(b.subscribers[chatID], ch)
	if len(b.subscribers[chatID]) == 0 {
		delete(b.subscribers, chatID)
	}
	b.mu.Unlock()
	close(ch)
}

func (b *ChatBroker) Publish(chatID int64, data []byte) {
	b.mu.RLock()
	for ch := range b.subscribers[chatID] {
		select {
		case ch <- data:
		default:
		}
	}
	b.mu.RUnlock()
}
