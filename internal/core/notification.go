package core

// Notification type constants.
const (
	NotifyRequestPending   = "request_pending"
	NotifyRequestApproved  = "request_approved"
	NotifyRequestDenied    = "request_denied"
	NotifyRequestFulfilled = "request_fulfilled"
)

// Notification is the persisted domain entity for an in-app notification.
type Notification struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	RequestID string `json:"requestId,omitempty"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"createdAt"`
}

// NotificationEvent is the WebSocket payload for a notification targeted at a specific user.
type NotificationEvent struct {
	TargetUserID string       `json:"targetUserId"`
	Notification Notification `json:"notification"`
}
