package streammanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	api "github.com/opplieam/bb-dist-noti/protogen/category_v1"
	"google.golang.org/protobuf/proto"
)

type Command interface {
	AddCommand(msg *api.CategoryMessage) error
	BroadcastCommand(msg *api.CategoryMessage) error
}

type Config struct {
	NatsAddr     string
	StreamName   string
	Description  string
	Subjects     []string
	ConsumerName string
}

type Manager struct {
	cmd      Command
	conn     *nats.Conn
	conCtx   jetstream.ConsumeContext
	js       jetstream.JetStream
	consumer jetstream.Consumer
	logger   *slog.Logger
}

func NewManager(ctx context.Context, cfg Config, cmd Command) (*Manager, error) {
	conn, err := nats.Connect(cfg.NatsAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to connect to streammanager: %w", err)
	}

	manager := &Manager{
		conn:   conn,
		js:     js,
		cmd:    cmd,
		logger: slog.With("component", "NATs"),
	}

	if err := manager.setupConsumer(ctx, cfg); err != nil {
		manager.Close()
		return nil, err
	}

	return manager, nil
}

func (m *Manager) Close() error {
	if m.conCtx != nil {
		m.conCtx.Stop()
	}
	if m.conn != nil {
		m.conn.Close()
	}
	return nil
}

func (m *Manager) setupConsumer(ctx context.Context, cfg Config) error {
	consumer, err := m.js.CreateOrUpdateConsumer(ctx, cfg.StreamName, jetstream.ConsumerConfig{
		Name:        cfg.ConsumerName,
		Durable:     cfg.ConsumerName,
		Description: cfg.Description,
		MaxDeliver:  4,
		BackOff: []time.Duration{
			5 * time.Second,
			10 * time.Second,
			15 * time.Second,
		},
		FilterSubjects: cfg.Subjects,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	m.consumer = consumer
	return nil
}

func (m *Manager) ConsumeMessages() error {
	m.logger.Info("consuming messages")
	ctx, err := m.consumer.Consume(func(msg jetstream.Msg) {
		var catMsg api.CategoryMessage
		_ = proto.Unmarshal(msg.Data(), &catMsg)
		err := m.cmd.AddCommand(&catMsg)
		if err != nil {
			m.logger.Error("failed to add command", "error", err)
		}
		err = m.cmd.BroadcastCommand(&catMsg)
		if err != nil {
			m.logger.Error("failed to broadcast command", "error", err)
		}

		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer stream: %w", err)
	}

	m.conCtx = ctx

	return nil
}
