package events

import "sync"

type EventEmitter struct {
	listeners map[string][]func(data any)
	mu        sync.RWMutex
}

func New() *EventEmitter {
	return &EventEmitter{
		listeners: make(map[string][]func(data any)),
	}
}

func (e *EventEmitter) On(event string, handler func(data any)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners[event] = append(e.listeners[event], handler)
}

func (e *EventEmitter) Emit(event string, data any) {
	e.mu.RLock()
	handlers := e.listeners[event]
	e.mu.RUnlock()

	for _, handler := range handlers {
		handler(data) // sync call; switch to goroutine for async
	}
}
