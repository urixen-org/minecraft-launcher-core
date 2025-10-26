package events

import "sync"

// EventEmitter provides a mechanism for event handling: registering listeners and emitting events.
// It is thread-safe using a sync.RWMutex.
type EventEmitter struct {
	// listeners maps event names (string) to a slice of handler functions.
	listeners map[string][]func(data any)
	// mu protects the listeners map from concurrent access.
	mu sync.RWMutex
}

// New creates and returns a new initialized EventEmitter.
func New() *EventEmitter {
	return &EventEmitter{
		listeners: make(map[string][]func(data any)),
	}
}

// On registers a handler function to be called whenever the specified event is emitted.
// Multiple handlers can be registered for the same event.
func (e *EventEmitter) On(event string, handler func(data any)) {
	e.mu.Lock() // Acquire write lock to modify the listeners map
	defer e.mu.Unlock()
	e.listeners[event] = append(e.listeners[event], handler)
}

// Emit executes all registered handlers for the specified event, passing the provided data.
// Handlers are called synchronously (in the same goroutine).
func (e *EventEmitter) Emit(event string, data any) {
	e.mu.RLock() // Acquire read lock to safely read the list of handlers
	// Note: The handlers slice is copied by value, allowing us to release the lock
	// before calling the handlers.
	handlers := e.listeners[event]
	e.mu.RUnlock()

	// Call each handler synchronously
	for _, handler := range handlers {
		handler(data)
	}
}
