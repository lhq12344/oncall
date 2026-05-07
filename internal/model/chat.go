package model

// ChatStreamInput is the internal input for streaming chat.
type ChatStreamInput struct {
	SessionID string
	Question  string
}

// ChatResumeInput is the internal input for resuming an interrupted chat stream.
type ChatResumeInput struct {
	SessionID      string
	CheckpointID   string
	InterruptIDs   []string
	Approved       *bool
	Resolved       *bool
	Comment        string
	SelectionValue string
}
