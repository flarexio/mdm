package inmem

import (
	"context"
	"regexp"
	"strings"
	"sync"

	"github.com/flarexio/core/pubsub"
)

// NewPubSub returns a synchronous in-process pubsub.PubSub for single-node use and
// tests. Unlike core's SimplePubSub (one goroutine per handler), Publish runs
// handlers inline, so a Notify() driving the durable event handler is read-your-
// write. A scaled deployment swaps in NATS behind the same interface.
func NewPubSub() pubsub.PubSub {
	return &syncPubSub{subscribers: make(map[string]*subscription)}
}

type subscription struct {
	pattern  *regexp.Regexp
	handlers []pubsub.MessageHandler
}

type syncPubSub struct {
	sync.RWMutex
	subscribers map[string]*subscription
}

func (ps *syncPubSub) Publish(topic string, data []byte) error {
	ps.RLock()
	defer ps.RUnlock()

	msg := &pubsub.Message{Topic: topic, Data: data}
	ctx := context.Background()

	for _, sub := range ps.subscribers {
		if !sub.pattern.MatchString(topic) {
			continue
		}
		for _, handler := range sub.handlers {
			if err := handler(ctx, msg); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ps *syncPubSub) Subscribe(topic string, callback pubsub.MessageHandler) error {
	ps.Lock()
	defer ps.Unlock()

	sub, ok := ps.subscribers[topic]
	if !ok {
		pattern, err := compilePattern(topic)
		if err != nil {
			return err
		}
		sub = &subscription{pattern: pattern}
		ps.subscribers[topic] = sub
	}

	sub.handlers = append(sub.handlers, callback)
	return nil
}

func (ps *syncPubSub) Close() error {
	ps.Lock()
	defer ps.Unlock()
	ps.subscribers = make(map[string]*subscription)
	return nil
}

// compilePattern turns a topic pattern into a regexp: "*" matches one word, "#"
// matches zero or more words. It mirrors core's SimplePubSub matching.
func compilePattern(pattern string) (*regexp.Regexp, error) {
	p := strings.ReplaceAll(pattern, `.`, `\.`)
	p = strings.ReplaceAll(p, `*`, `[^.]+`)
	p = strings.ReplaceAll(p, `#`, `.*`)
	return regexp.Compile("^" + p + "$")
}
