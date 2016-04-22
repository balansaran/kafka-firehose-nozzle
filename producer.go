package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/cloudfoundry/sonde-go/events"
	"golang.org/x/net/context"
)

type NozzleProducer interface {
	// Produce produces firehose events
	Produce(context.Context, <-chan *events.Envelope)

	// Errors returns error channel
	Errors() <-chan error

	// Close shuts down the producer and flushes any messages it may have buffered.
	Close() error
}

// LogProducer implements NozzleProducer interfaces.
// This producer is mainly used for debugging reason.
type LogProducer struct {
	Logger *log.Logger

	once sync.Once
}

var defaultLogger = log.New(os.Stdout, "", log.LstdFlags)

// init sets default logger
func (p *LogProducer) init() {
	if p.Logger == nil {
		p.Logger = defaultLogger
	}
}

func (p *LogProducer) Produce(ctx context.Context, eventCh <-chan *events.Envelope) {
	p.once.Do(p.init)
	for {
		select {
		case event := <-eventCh:
			buf, _ := json.Marshal(event)
			p.Logger.Printf("[INFO] %s", string(buf))
		case <-ctx.Done():
			// Stop process immediately
			return
		}
	}
}

func (p *LogProducer) Errors() <-chan error {
	errCh := make(chan error, 1)
	return errCh
}

func (p *LogProducer) Close() error {
	// Nothing to close for thi producer
	return nil
}
