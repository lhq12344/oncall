package team

import "testing"

func TestMailboxMessagesCarrySequenceAndTrace(t *testing.T) {
	var mailbox Mailbox
	msg := mailbox.Send(Message{Sender: "leader", Recipient: "worker", TaskID: "t1", TraceID: "trace", Content: "collect logs"})
	if msg.Sequence != 1 || msg.TraceID != "trace" || msg.TaskID != "t1" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestTeamCannotApproveMutation(t *testing.T) {
	coord := NewCoordinator()
	if err := coord.DelegateTask("t1"); err != nil {
		t.Fatal(err)
	}
	if coord.Status("t1") != TaskPending {
		t.Fatalf("unexpected status")
	}
	if err := coord.ApproveMutation(); err == nil {
		t.Fatal("team must not approve mutation")
	}
}
