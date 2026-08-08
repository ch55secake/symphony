package events

// ApprovalRequestedPayload records an action awaiting operator authorization.
type ApprovalRequestedPayload struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
	Summary     string `json:"summary"`
	Hash        string `json:"hash"`
}

// ApprovalGrantedPayload records an operator authorization.
type ApprovalGrantedPayload struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
}

// ApprovalDeniedPayload records an operator denial without a raw explanation.
type ApprovalDeniedPayload struct {
	OperationID string `json:"operation_id"`
	Action      string `json:"action"`
	ReasonCode  string `json:"reason_code"`
}
