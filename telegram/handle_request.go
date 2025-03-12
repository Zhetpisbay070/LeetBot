package telegram

import (
	"net/http"
)

type AptekaPayload struct {
	Name      string   `json:"name"`
	Phone     string   `json:"phone"`
	Address   string   `json:"address"`
	Medicines []string `json:"medicines"`
}

func (lp *LongPoll) GetRequest(w http.ResponseWriter, r *http.Request) {
}
