package models

type WSMessageType string

const (
	WSTypeSubscribe   WSMessageType = "subscribe"
	WSTypeUnsubscribe WSMessageType = "unsubscribe"
	WSTypeOddsUpdate  WSMessageType = "odds_update"
	WSTypeScoreUpdate WSMessageType = "score_update"
	WSTypeBetAck      WSMessageType = "bet_ack"
	WSTypeBetUpdate   WSMessageType = "bet_update"
	WSTypeError       WSMessageType = "error"
	WSTypePing        WSMessageType = "ping"
	WSTypePong        WSMessageType = "pong"
)

type WSMessage struct {
	Type    WSMessageType `json:"type"`
	Payload interface{}   `json:"payload,omitempty"`
}

type WSSubscribePayload struct {
	MarketIDs []string `json:"market_ids"`
}

type WSBetAckPayload struct {
	BetID   string  `json:"bet_id"`
	Status  string  `json:"status"`
	Filled  float64 `json:"filled"`
	Message string  `json:"message,omitempty"`
}
