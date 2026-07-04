package ai

type Message struct {
	MessageID      string
	ConversationID string
	Seq            int64
	FromUID        string
	Content        string
	CreatedAt      int64
}

type GenerateSummaryParams struct {
	GroupID      string
	OperatorID   string
	MessageLimit int
	IncludeTodos bool
	IncludeRisks bool
}

type SummaryRequest struct {
	GroupID        string
	ConversationID string
	OperatorID     string
	Messages       []Message
	IncludeTodos   bool
	IncludeRisks   bool
}

type TodoItem struct {
	Title     string `json:"title"`
	OwnerID   string `json:"owner_id,omitempty"`
	SourceSeq int64  `json:"source_seq,omitempty"`
}

type RiskItem struct {
	Level       string `json:"level"`
	Description string `json:"description"`
	SourceSeq   int64  `json:"source_seq,omitempty"`
}

type SummaryResult struct {
	SummaryID       string
	GroupID         string
	ConversationID  string
	MessageStartSeq int64
	MessageEndSeq   int64
	Summary         string
	Todos           []TodoItem
	Risks           []RiskItem
	Provider        string
	CreatedAt       int64
}
