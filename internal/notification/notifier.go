package notification

import (
	"context"
	"fmt"
	"log"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/request"
)

const topicNotification = "notification"

// Bus is the minimal event-bus interface the Notifier needs.
// *events.Bus satisfies it.
type Bus interface {
	Subscribe(topic string) (<-chan events.Event, func())
	Publish(events.Event)
}

// AuthLister can enumerate user IDs for users that have the manage_requests
// capability. The concrete implementation wraps auth.Service; tests supply a fake.
type AuthLister interface {
	ListManagerIDs(ctx context.Context) ([]string, error)
}

// Notifier subscribes to request lifecycle events and creates notification rows,
// then re-publishes targeted "notification" events so the WebSocket layer can push
// them to connected clients. It mirrors the structure of request.Tracker.
type Notifier struct {
	bus    Bus
	svc    *Service
	auth   AuthLister
	cancel context.CancelFunc
	done   chan struct{}
	unsubs []func()
}

// NewNotifier constructs a Notifier. Call Start to begin processing.
func NewNotifier(bus Bus, svc *Service, auth AuthLister) *Notifier {
	return &Notifier{
		bus:  bus,
		svc:  svc,
		auth: auth,
		done: make(chan struct{}),
	}
}

// Start subscribes to request.created, request.updated, and request.canceled
// and begins processing events in a background goroutine. The provided ctx
// controls the lifecycle alongside Stop(). Safe to call only once.
func (n *Notifier) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	n.cancel = cancel

	createdCh, unsubCreated := n.bus.Subscribe(request.TopicCreated)
	updatedCh, unsubUpdated := n.bus.Subscribe(request.TopicUpdated)
	canceledCh, unsubCanceled := n.bus.Subscribe(request.TopicCanceled)
	n.unsubs = []func(){unsubCreated, unsubUpdated, unsubCanceled}

	go func() {
		defer close(n.done)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-createdCh:
				if !ok {
					return
				}
				n.handleCreated(ctx, ev)
			case ev, ok := <-updatedCh:
				if !ok {
					return
				}
				n.handleUpdated(ctx, ev)
			case ev, ok := <-canceledCh:
				if !ok {
					return
				}
				n.handleCanceled(ctx, ev)
			}
		}
	}()
}

// Stop cancels the background goroutine and unsubscribes from the bus.
func (n *Notifier) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
	for _, unsub := range n.unsubs {
		unsub()
	}
	<-n.done
}

// handleCreated processes a request.created event: fan out a request_pending
// notification to every user with the manage_requests capability.
func (n *Notifier) handleCreated(ctx context.Context, ev events.Event) {
	re, ok := ev.Payload.(core.RequestEvent)
	if !ok {
		return
	}
	req := re.Request

	managers, err := n.auth.ListManagerIDs(ctx)
	if err != nil {
		log.Printf("notifier: ListManagerIDs: %v", err)
		return
	}

	title := "New request"
	body := fmt.Sprintf("%s requested %s", req.RequestedBy, itemTitle(req))

	for _, managerID := range managers {
		notif, err := n.svc.Create(ctx, core.Notification{
			UserID:    managerID,
			Type:      core.NotifyRequestPending,
			Title:     title,
			Body:      body,
			RequestID: req.ID,
		})
		if err != nil {
			log.Printf("notifier: Create notification for manager %q: %v", managerID, err)
			continue
		}
		n.bus.Publish(events.Event{
			Topic: topicNotification,
			Payload: core.NotificationEvent{
				TargetUserID: managerID,
				Notification: notif,
			},
		})
	}
}

// handleUpdated processes a request.updated event: for terminal statuses that
// the requester cares about (approved / denied / fulfilled), create a notification
// for the requester, publish it, and resolve pending manager notifications.
func (n *Notifier) handleUpdated(ctx context.Context, ev events.Event) {
	re, ok := ev.Payload.(core.RequestEvent)
	if !ok {
		return
	}
	req := re.Request

	notifType, handled := terminalNotifType(req.Status)
	if !handled {
		return
	}

	title, body := requesterMessage(req)

	notif, err := n.svc.Create(ctx, core.Notification{
		UserID:    req.RequestedBy,
		Type:      notifType,
		Title:     title,
		Body:      body,
		RequestID: req.ID,
	})
	if err != nil {
		log.Printf("notifier: Create notification for requester %q: %v", req.RequestedBy, err)
	} else {
		n.bus.Publish(events.Event{
			Topic: topicNotification,
			Payload: core.NotificationEvent{
				TargetUserID: req.RequestedBy,
				Notification: notif,
			},
		})
	}

	if err := n.svc.ResolvePendingForRequest(ctx, req.ID); err != nil {
		log.Printf("notifier: ResolvePendingForRequest(%q): %v", req.ID, err)
	}
}

// handleCanceled processes a request.canceled event: resolves manager badges for
// the canceled request. No notification is created for anyone — the requester
// canceled themselves and does not need a notification.
func (n *Notifier) handleCanceled(ctx context.Context, ev events.Event) {
	re, ok := ev.Payload.(core.RequestEvent)
	if !ok {
		return
	}
	if err := n.svc.ResolvePendingForRequest(ctx, re.Request.ID); err != nil {
		log.Printf("notifier: ResolvePendingForRequest(%q) on cancel: %v", re.Request.ID, err)
	}
}

// terminalNotifType maps a request status to the notification type the requester
// receives. Returns ("", false) for non-terminal or unknown statuses.
func terminalNotifType(status string) (string, bool) {
	switch status {
	case core.RequestApproved:
		return core.NotifyRequestApproved, true
	case core.RequestDenied:
		return core.NotifyRequestDenied, true
	case core.RequestFulfilled:
		return core.NotifyRequestFulfilled, true
	default:
		return "", false
	}
}

// itemTitle returns the display title for a request, preferring the title field.
func itemTitle(req core.Request) string {
	if req.Title != "" {
		return req.Title
	}
	if req.Album != "" {
		return req.Album
	}
	return req.ExternalID
}

// requesterMessage returns the title + body for a terminal-status notification
// sent to the requester.
func requesterMessage(req core.Request) (title, body string) {
	t := itemTitle(req)
	switch req.Status {
	case core.RequestApproved:
		return "Request approved", fmt.Sprintf("Your request for %s was approved", t)
	case core.RequestDenied:
		return "Request denied", fmt.Sprintf("Your request for %s was denied", t)
	case core.RequestFulfilled:
		return "Request fulfilled", fmt.Sprintf("Your request for %s was fulfilled", t)
	default:
		return "Request updated", fmt.Sprintf("Your request for %s was updated", t)
	}
}
