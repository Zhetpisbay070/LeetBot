package entity

type SessionStatus uint8

const (
	SessionStatusOpen SessionStatus = iota
	SessionStatusClosed
)

type Session struct {
	ID        int           `bson:"_id,omitempty"`
	UserID    string        `bson:"user_id,omitempty"`
	Status    SessionStatus `bson:"status,omitempty"`
	StartTime int64         `bson:"start_time,omitempty"`
	EndTime   int64         `bson:"end_time,omitempty"`
}
