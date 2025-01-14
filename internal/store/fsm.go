package store

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/raft"
	"github.com/opplieam/bb-dist-noti/internal/clientstate"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	"google.golang.org/protobuf/proto"
)

type CommandType uint8

const (
	CommandTypeAdd       CommandType = 0
	CommandTypeBroadcast CommandType = 1
)

var _ raft.FSM = (*FiniteState)(nil)

// FiniteState represents the finite state machine (FSM)
// used to manage category messages in the distributed notification system.
type FiniteState struct {
	mu          sync.RWMutex
	history     []*api.CategoryMessage
	clientState *clientstate.ClientState
	limit       int
	batchSize   int
}

// NewFiniteState creates a new FSM instance.
func NewFiniteState(limit int, cState *clientstate.ClientState) *FiniteState {
	if limit <= 0 {
		limit = 500
	}
	var batchSize = limit / 4
	return &FiniteState{
		history:     make([]*api.CategoryMessage, 0, limit),
		limit:       limit,
		clientState: cState,
		batchSize:   batchSize,
	}
}

// Apply is invoked when Raft applies a log entry to the FSM.
// It returns either nil (indicating successful application) or an error.
func (s *FiniteState) Apply(record *raft.Log) interface{} {
	buf := record.Data
	commandType := CommandType(buf[0])

	var msg api.CategoryMessage
	err := proto.Unmarshal(buf[1:], &msg)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch commandType {
	case CommandTypeAdd:
		if len(s.history) >= s.limit {
			newHistory := make([]*api.CategoryMessage, len(s.history)-s.batchSize, s.limit)
			copy(newHistory, s.history[s.batchSize:])
			s.history = newHistory
		}
		s.history = append(s.history, &msg)
		return nil
	case CommandTypeBroadcast:
		for _, clientCh := range s.clientState.GetAllClients() {
			clientCh <- &msg
		}
		return nil
	default:
		return fmt.Errorf("unknown command type %d", commandType)
	}
}

// Snapshot is used to support log compaction.
func (s *FiniteState) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := &api.CategorySnapshot{
		Messages: make([]*api.CategoryMessage, len(s.history)),
	}

	// Deep copy messages
	for i, msg := range s.history {
		snapshot.Messages[i] = proto.Clone(msg).(*api.CategoryMessage)
	}

	return &FSMSnapshot{snapshot: snapshot}, nil
}

// Restore is used to restore an FSM from a snapshot.
func (s *FiniteState) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var buf bytes.Buffer
	_, err := io.Copy(&buf, rc)
	if err != nil {
		return fmt.Errorf("error reading snapshot: %w", err)
	}

	// Get the byte slice from the buffer
	data := buf.Bytes()

	var snapshot api.CategorySnapshot
	if err = proto.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("error parsing snapshot: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Restore history from snapshot
	s.history = make([]*api.CategoryMessage, len(snapshot.GetMessages()))
	for i, msg := range snapshot.GetMessages() {
		s.history[i] = proto.Clone(msg).(*api.CategoryMessage)
	}

	return nil
}

/*
Read returns the n latest CategoryMessages.

	Example usage:
	fsm := NewFiniteState(500)
	latestMessages := fsm.Read(3)
	fmt.Println("Latest 3 Messages:", latestMessages)
*/
func (s *FiniteState) Read(n int) []*api.CategoryMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	startIndex := 0
	if len(s.history) > n {
		startIndex = len(s.history) - n
	}

	// Deep copy
	copiedHistory := make([]*api.CategoryMessage, n)
	for i := 0; i < n && startIndex+i < len(s.history); i++ {
		copiedHistory[i] = proto.Clone(s.history[startIndex+i]).(*api.CategoryMessage)
	}

	return copiedHistory
}

/*
ReadLatest returns the most recent CategoryMessage.

	Example usage:
	fsm := NewFiniteState(500)
	latestMessage := fsm.ReadLatest()
	fmt.Println("Latest Message:", latestMessage)
*/
func (s *FiniteState) ReadLatest() *api.CategoryMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.history) == 0 {
		return nil
	}

	// Deep copy
	latestMsg := proto.Clone(s.history[len(s.history)-1]).(*api.CategoryMessage)

	return latestMsg
}

var _ raft.FSMSnapshot = (*FSMSnapshot)(nil)

// FSMSnapshot is used to provide a snapshot of the current state.
type FSMSnapshot struct {
	snapshot *api.CategorySnapshot
}

// Persist should dump all necessary state to the WriteCloser 'sink',
// and call sink.Close() when finished or call sink.Cancel() on error.
func (f *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	// Marshal snapshot to bytes
	data, err := proto.Marshal(f.snapshot)
	if err != nil {
		_ = sink.Cancel()
		return err
	}

	// Write data to sink
	if _, err = sink.Write(data); err != nil {
		_ = sink.Cancel()
		return err
	}

	return sink.Close()
}

// Release is invoked when we are finished with the snapshot.
func (f *FSMSnapshot) Release() {}

// newCommand Helper function to create a new command.
func newCommand(commandType CommandType, msg *api.CategoryMessage) ([]byte, error) {
	var buf bytes.Buffer
	// Write command type as a byte
	err := buf.WriteByte(byte(commandType))
	if err != nil {
		return nil, err
	}
	// Marshal the message and write it to the buffer
	msgData, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(msgData)
	if err != nil {
		return nil, err
	}
	// Convert the buffer to a byte slice
	return buf.Bytes(), nil
}
