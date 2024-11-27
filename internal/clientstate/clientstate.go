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

func NewClientState() *ClientState {
	return &ClientState{
		clients: make(map[string]chan *api.CategoryMessage),
	}
}

func (s *ClientState) Close() error {
	for _, client := range s.clients {
		close(client)
	}
	return nil
}

func (s *ClientState) AddClient(id string) chan *api.CategoryMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[id] = make(chan *api.CategoryMessage)
	return s.clients[id]
}

func (s *ClientState) RemoveClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, id)
}

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
