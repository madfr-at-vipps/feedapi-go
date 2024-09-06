package feedapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"

	"github.com/sirupsen/logrus"
)

var logger *logrus.Logger

func init() {
	logger = logrus.StandardLogger()
	logger.SetLevel(logrus.DebugLevel)
}

func Server(publisher EventPublisher) *httptest.Server {
	handlers := NewHTTPHandlers(publisher, func(*http.Request) logrus.FieldLogger {
		return logger
	})

	routingHandler := func(w http.ResponseWriter, r *http.Request) {
		// expose the feed on "testfeed"
		if r.URL.Path == "/testfeed" {
			handlers.DiscoveryHandler(w, r)
			return
		} else if r.URL.Path == "/testfeed/events" {
			handlers.EventsHandler(w, r)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}

	return httptest.NewServer(http.HandlerFunc(routingHandler))
}

type TestFeedAPI struct {
	partitions map[int][]TestEvent
}

func NewTestFeedAPI() *TestFeedAPI {
	api := TestFeedAPI{partitions: map[int][]TestEvent{}}
	partition0 := make([]TestEvent, 10000)
	partition1 := make([]TestEvent, 10000)
	for i := 0; i < 5000; i++ {
		partition0[i] = TestEvent{
			ID:      fmt.Sprintf("00000000-0000-0000-0000-%012x", i),
			Version: 0,
			Cursor:  i,
			Type:    "PaymentCaptured",
		}
		partition1[i] = TestEvent{
			ID:      fmt.Sprintf("11111111-0000-0000-0000-%012x", i),
			Version: 0,
			Cursor:  i,
			Type:    "PaymentCaptured",
		}
	}
	for i := 5000; i < 10000; i++ {
		partition0[i] = TestEvent{
			ID:      fmt.Sprintf("00000000-0000-0000-0000-%012x", i),
			Version: 0,
			Cursor:  i,
			Type:    "PaymentCancelled",
		}
		partition1[i] = TestEvent{
			ID:      fmt.Sprintf("11111111-0000-0000-0000-%012x", i),
			Version: 0,
			Cursor:  i,
			Type:    "PaymentCancelled",
		}
	}
	api.partitions[0] = partition0
	api.partitions[1] = partition1
	return &api
}

func (t TestFeedAPI) GetName() string {
	return "TestFeedAPI"
}

func (t TestFeedAPI) GetFeedInfo() FeedInfo {
	return FeedInfo{
		Token: "the-token",
		Partitions: []Partition{
			{
				Id: 0,
			},
			{
				Id: 1,
			},
		},
	}
}

func (t TestFeedAPI) FetchEvents(ctx context.Context, token string, partitionID int, cursor string, receiver EventReceiver, options Options) error {
	if options.PageSizeHint == DefaultPageSize {
		options.PageSizeHint = 100
	}
	partition, ok := t.partitions[partitionID]
	if !ok {
		return ErrPartitionDoesntExist
	}
	var err error
	var lastProcessedCursor int
	switch cursor {
	case FirstCursor:
		lastProcessedCursor = -100
	case LastCursor:
		lastProcessedCursor = len(partition) - 2
	// Mock responses: set the cursor to one of the following values to get a mocked response.
	case cursorReturn500:
		return err500
	case cursorReturn504:
		return err504
	default:
		lastProcessedCursor, err = strconv.Atoi(cursor)
		if err != nil {
			return err
		}
	}
	eventsProcessed := 0
	for _, event := range partition {
		if event.Cursor > lastProcessedCursor {
			if options.EventTypes == nil || slices.Contains(options.EventTypes, event.Type) {
				if err := receiver.Event(mustMarshalJson(partition[event.Cursor])); err != nil {
					return err
				}
			}

			if err := receiver.Checkpoint(fmt.Sprintf("%d", event.Cursor)); err != nil {
				return err
			}
			lastProcessedCursor = event.Cursor
			eventsProcessed++
		}
		if eventsProcessed == options.PageSizeHint {
			break
		}
	}
	return nil
}

func mustMarshalJson(e any) json.RawMessage {
	result, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	return result
}
