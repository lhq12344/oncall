package skills

type Source string

const (
	SourceSystem  Source = "system"
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

type Metadata struct {
	Name         string
	Description  string
	Version      string
	Source       Source
	Path         string
	Triggers     []string
	AllowedTools []string
}

type Skill struct {
	Metadata Metadata
	Content  string
}
