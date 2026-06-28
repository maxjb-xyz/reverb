package core

// Request status constants.
const (
	RequestPending   = "pending"
	RequestApproved  = "approved"
	RequestDenied    = "denied"
	RequestFulfilled = "fulfilled"
	RequestFailed    = "failed"
)

// RequestItem is the descriptor provided when creating a request.
type RequestItem struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album,omitempty"`
	ISRC       string `json:"isrc,omitempty"`
	DurationMs int    `json:"durationMs,omitempty"`
	CoverArtID string `json:"coverArtId,omitempty"`
	CoverUrl   string `json:"coverUrl,omitempty"`
	Kind       string `json:"kind,omitempty"`
}

// Request is the persisted domain entity for a user's download request.
type Request struct {
	ID            string `json:"id"`
	RequestedBy   string `json:"requestedBy"`
	Source        string `json:"source"`
	ExternalID    string `json:"externalId"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Album         string `json:"album,omitempty"`
	ISRC          string `json:"isrc,omitempty"`
	DurationMs    int    `json:"durationMs,omitempty"`
	CoverArtID    string `json:"coverArtId,omitempty"`
	CoverUrl      string `json:"coverUrl,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"createdAt"`
	DecidedBy     string `json:"decidedBy,omitempty"`
	DecidedAt     int64  `json:"decidedAt,omitempty"`
	DownloadJobID string `json:"downloadJobId,omitempty"`
	DenyReason    string `json:"denyReason,omitempty"`
}

// RequestEvent is the WebSocket payload published on request.created / request.updated.
// TargetUserID routes the event to a specific user; ForManagers broadcasts to managers.
type RequestEvent struct {
	Request      Request `json:"request"`
	TargetUserID string  `json:"targetUserId,omitempty"`
	ForManagers  bool    `json:"forManagers,omitempty"`
}
