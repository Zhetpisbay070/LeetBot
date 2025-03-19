package entity

type Message struct {
	ID        string `bson:"_id,omitempty"`
	SessionID string `bson:"session_id,omitempty"`
	TimeStamp int64  `bson:"timestamp,omitempty"`
	UserID    string `bson:"user_id,omitempty"`
	Text      string `bson:"text,omitempty"`
}
