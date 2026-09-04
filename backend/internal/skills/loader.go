package skills

import (
	"os"
	"strings"
)

func Load(root string, meta Metadata) (Skill, error) {
	path, err := ResolveUnderRoot(root, meta.Path)
	if err != nil {
		return Skill{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return Skill{Metadata: meta, Content: strings.TrimSpace(string(b))}, nil
}
