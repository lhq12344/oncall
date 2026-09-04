package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"go_agent/internal/workflow/incident"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("replayctl", flag.ContinueOnError)
	suite := fs.String("suite", "testdata/replay/incident", "fixture directory or jsonl file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := *suite
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "phase10_incident_replay.jsonl")
	}
	fixtures, err := readFixtures(path)
	if err != nil {
		return err
	}
	passed := 0
	for _, fixture := range fixtures {
		state, err := incident.Runtime{}.Run(ctx, fixture)
		if err != nil {
			return err
		}
		if state.Terminal != fixture.ExpectedTerminal {
			return fmt.Errorf("%s terminal=%s want %s", fixture.ID, state.Terminal, fixture.ExpectedTerminal)
		}
		passed++
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "passed": passed})
}

func readFixtures(path string) ([]incident.Fixture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var fixtures []incident.Fixture
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var fixture incident.Fixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			return nil, err
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, scanner.Err()
}
