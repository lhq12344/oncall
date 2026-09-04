package catalog

import "fmt"

func MigrateStateVersion(from, to string, state []byte) ([]byte, error) {
	if from == to || from == "" {
		return append([]byte(nil), state...), nil
	}
	return nil, fmt.Errorf("state migration from %s to %s is not available", from, to)
}
