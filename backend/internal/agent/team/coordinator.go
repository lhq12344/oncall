package team

import "fmt"

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskCancelled TaskStatus = "cancelled"
)

type Coordinator struct {
	Mailbox Mailbox
	Tasks   map[string]TaskStatus
}

func NewCoordinator() *Coordinator { return &Coordinator{Tasks: map[string]TaskStatus{}} }

func (c *Coordinator) DelegateTask(id string) error {
	if c.Tasks == nil {
		c.Tasks = map[string]TaskStatus{}
	}
	if _, exists := c.Tasks[id]; exists {
		return fmt.Errorf("task already exists")
	}
	c.Tasks[id] = TaskPending
	return nil
}

func (c *Coordinator) Status(id string) TaskStatus { return c.Tasks[id] }

func (c *Coordinator) Cancel(id string) { c.Tasks[id] = TaskCancelled }

func (c *Coordinator) ApproveMutation() error {
	return fmt.Errorf("team cannot approve or execute mutation")
}
