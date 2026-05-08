package dialogue

import (
	"context"

	einoModel "github.com/cloudwego/eino/components/model"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

type fakeToolCallingChatModel struct {
	generateResponse *schema.Message
	generateErr      error
	lastGenerate     []*schema.Message
}

func (f *fakeToolCallingChatModel) Generate(_ context.Context, input []*schema.Message, _ ...einoModel.Option) (*schema.Message, error) {
	f.lastGenerate = cloneMessages(input)
	if f.generateErr != nil {
		return nil, f.generateErr
	}
	if f.generateResponse != nil {
		return f.generateResponse, nil
	}
	return schema.AssistantMessage("", nil), nil
}

func (f *fakeToolCallingChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...einoModel.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](1)
	go func() {
		defer sw.Close()
	}()
	return sr, nil
}

func (f *fakeToolCallingChatModel) WithTools(_ []*schema.ToolInfo) (einoModel.ToolCallingChatModel, error) {
	return f, nil
}

type fakeRetriever struct {
	queries []string
	docs    map[string][]*schema.Document
}

func (f *fakeRetriever) Retrieve(_ context.Context, query string, _ ...einoretriever.Option) ([]*schema.Document, error) {
	f.queries = append(f.queries, query)
	if f.docs == nil {
		return nil, nil
	}
	return f.docs[query], nil
}

func cloneMessages(input []*schema.Message) []*schema.Message {
	if len(input) == 0 {
		return nil
	}
	out := make([]*schema.Message, 0, len(input))
	for _, msg := range input {
		if msg == nil {
			out = append(out, nil)
			continue
		}
		cloned := *msg
		out = append(out, &cloned)
	}
	return out
}
