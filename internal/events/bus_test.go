package events

import (
	"testing"
	"time"
)

func TestSubscribeReceivesPublished(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("download.progress")
	defer cancel()

	b.Publish(Event{Topic: "download.progress", Payload: 42})

	select {
	case ev := <-ch:
		if ev.Payload.(int) != 42 {
			t.Fatalf("payload = %v", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("t")
	cancel()
	b.Publish(Event{Topic: "t", Payload: 1})
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		// no delivery — acceptable
	}
}

func TestPublishToOtherTopicIgnored(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("a")
	defer cancel()
	b.Publish(Event{Topic: "b", Payload: 1})
	select {
	case <-ch:
		t.Fatal("got event for wrong topic")
	case <-time.After(100 * time.Millisecond):
	}
}
