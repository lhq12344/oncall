package toolregistry

type ToolFactory func() (BaseTool, error)

type RegisteredTool struct {
	Descriptor ToolDescriptor
	Factory    ToolFactory
}
