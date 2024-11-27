// Package clientstate provides functionality to manage and store user connections.
package clientstate

import (
	"sync"

	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
)

type Clients map[string]chan *api.CategoryMessage

type ClientState struct {
	mu      sync.RWMutex
	clients Clients
}

// NewClientState creates a new instance of ClientState with an empty clients map.
func NewClientState() *ClientState {
	return &ClientState{
		clients: make(map[string]chan *api.CategoryMessage),
	}
}

// Close closes all client channels and removes them from the clients map.
func (s *ClientState) Close() error {
	for _, client := range s.clients {
		close(client)
	}
	return nil
}

// AddClient adds a new client with the specified ID to the clients map and returns the corresponding message channel.
func (s *ClientState) AddClient(id string) chan *api.CategoryMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[id] = make(chan *api.CategoryMessage)
	return s.clients[id]
}

// RemoveClient removes the client with the specified ID from the clients map.
func (s *ClientState) RemoveClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, id)
}

// GetAllClients returns a copy of the current clients map.
// This ensures that the original map can be safely modified without affecting external access.
func (s *ClientState) GetAllClients() Clients {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Create a copy of the map
	copiedClients := make(Clients)
	for key, value := range s.clients {
		copiedClients[key] = value
	}
	return copiedClients
}
