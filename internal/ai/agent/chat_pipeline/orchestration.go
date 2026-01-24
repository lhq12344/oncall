package chat_pipeline

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func BuildChatAgent(ctx context.Context) (r compose.Runnable[*UserMessage, *schema.Message], err error) {
	const (
		InputToRag      = "InputToRag"
		ChatTemplate    = "ChatTemplate"
		ReactAgent      = "ReactAgent"
		MilvusRetriever = "MilvusRetriever"
		InputToChat     = "InputToChat"
		MergeInputs     = "MergeInputs"
	)
	g := compose.NewGraph[*UserMessage, *schema.Message]()
	_ = g.AddLambdaNode(InputToRag, compose.InvokableLambdaWithOption(newInputToRagLambda), compose.WithNodeName("UserMessageToRag"))
	_ = g.AddLambdaNode(InputToChat, compose.InvokableLambdaWithOption(newInputToChatLambda), compose.WithNodeName("UserMessageToChat"))
	_ = g.AddLambdaNode(MergeInputs, compose.InvokableLambdaWithOption(newMergeLambda), compose.WithNodeName("MergeInputs"))
	chatTemplateKeyOfChatTemplate, err := newChatTemplate(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddChatTemplateNode(ChatTemplate, chatTemplateKeyOfChatTemplate)
	reactAgentKeyOfLambda, err := newReactAgentLambda(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddLambdaNode(ReactAgent, reactAgentKeyOfLambda, compose.WithNodeName("ReActAgent"))
	milvusRetrieverKeyOfRetriever, err := newRetriever(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddRetrieverNode(MilvusRetriever, milvusRetrieverKeyOfRetriever, compose.WithOutputKey("documents"))
	// 注意上面的 output key 设置，把查询出来的设置为了documents，匹配 ChatTemplate 里面说prompt
	_ = g.AddEdge(compose.START, InputToRag)
	_ = g.AddEdge(compose.START, InputToChat)
	_ = g.AddEdge(InputToRag, MilvusRetriever)
	_ = g.AddEdge(MilvusRetriever, MergeInputs)
	_ = g.AddEdge(InputToChat, MergeInputs)
	_ = g.AddEdge(MergeInputs, ChatTemplate)
	_ = g.AddEdge(ChatTemplate, ReactAgent)
	_ = g.AddEdge(ReactAgent, compose.END)
	r, err = g.Compile(ctx, compose.WithGraphName("ChatAgent"), compose.WithNodeTriggerMode(compose.AllPredecessor))
	if err != nil {
		return nil, err
	}
	return r, err
}
