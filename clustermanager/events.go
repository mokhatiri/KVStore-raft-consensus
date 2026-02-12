package clustermanager

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"distributed-kv/types"
)

type EventBuffer struct {
	events  []types.RPCEvent
	maxSize int
	mu      sync.RWMutex
}

type EventFileWriter struct {
	outputDir string
	ticker    *time.Ticker
	eventsCh  <-chan types.RPCEvent
}

func NewEventBuffer(maxSize int) *EventBuffer {
	return &EventBuffer{
		events:  make([]types.RPCEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

func NewEventFileWriter(outputDir string, eventsCh <-chan types.RPCEvent) *EventFileWriter {
	return &EventFileWriter{
		outputDir: outputDir,
		ticker:    time.NewTicker(EventFlushInterval),
		eventsCh:  eventsCh,
	}
}

/* EventBuffer methods */

func (eb *EventBuffer) AddEvent(event types.RPCEvent) {
	// add an event to the buffer, evicting old events if we exceed max size
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if len(eb.events) >= eb.maxSize {
		eb.events = eb.events[1:] // evict oldest event
	}
	eb.events = append(eb.events, event)
}

func (eb *EventBuffer) GetLast(limit int) []types.RPCEvent {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	if limit > len(eb.events) {
		limit = len(eb.events)
	}
	return append([]types.RPCEvent(nil), eb.events[len(eb.events)-limit:]...)
}

func (eb *EventBuffer) GetAllEvents() []types.RPCEvent {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return append([]types.RPCEvent(nil), eb.events...)
}

/* EventFileWriter methods */

func (efw *EventFileWriter) flushToFile(events []types.RPCEvent) {
	// use the efw.outputDir and current timestamp to create a new file and write the events to it

	// check the dir exists, if not create it
	if _, err := os.Stat(efw.outputDir); os.IsNotExist(err) {
		err := os.MkdirAll(efw.outputDir, 0755)
		if err != nil {
			fmt.Printf("Error creating output directory: %v\n", err)
			return
		}
	}

	// create a new file with timestamp
	filename := fmt.Sprintf("%s/events_%d.log", efw.outputDir, time.Now().Unix())
	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating event file: %v\n", err)
		return
	}
	defer file.Close()

	// write events to the file
	for _, event := range events {
		// use encoding/json to marshal the event to a string
		eventData, err := json.Marshal(event)

		if err != nil {
			fmt.Printf("Error marshaling event: %v\n", err)
			return
		}
		_, err = file.WriteString(fmt.Sprintf("%s\n", eventData))
		if err != nil {
			fmt.Printf("Error writing to event file: %v\n", err)
			return
		}
	}

	fmt.Printf("Flushed %d events to file: %s\n", len(events), filename)
}

func (efw *EventFileWriter) Start() {
	// start a goroutine to listen for events and flush them to files at intervals
	go func() {
		var batch []types.RPCEvent
		for {
			select {
			case event := <-efw.eventsCh:
				batch = append(batch, event) // add event to batch
			case <-efw.ticker.C: // when the ticker ticks, flush the batch to a file
				if len(batch) > 0 {
					efw.flushToFile(batch)
					batch = nil
				}
			}
		}
	}()
}
