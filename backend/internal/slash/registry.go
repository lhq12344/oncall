package slash

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	commands map[string]Command
	aliases  map[string]string
	builtins map[string]bool
	warnings []string
}

func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
		aliases:  make(map[string]string),
		builtins: make(map[string]bool),
	}
}

func (r *Registry) Register(cmd Command) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	cmd.Name = normalizeName(cmd.Name)
	aliases := make([]string, 0, len(cmd.Aliases))
	for _, alias := range cmd.Aliases {
		alias = normalizeAlias(alias)
		if alias != "" && alias != cmd.Name {
			aliases = append(aliases, alias)
		}
	}
	cmd.Aliases = uniqueStrings(aliases)
	if err := ensureHandler(cmd); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.builtins[cmd.Name] && !cmd.Builtin {
		return fmt.Errorf("command %q conflicts with builtin", cmd.Name)
	}
	if existing, ok := r.commands[cmd.Name]; ok {
		if existing.Builtin && !cmd.Builtin {
			return fmt.Errorf("command %q conflicts with builtin", cmd.Name)
		}
		return fmt.Errorf("command %q already registered", cmd.Name)
	}
	if target, ok := r.aliases[cmd.Name]; ok {
		if r.builtins[target] && !cmd.Builtin {
			return fmt.Errorf("command %q conflicts with builtin alias", cmd.Name)
		}
		return fmt.Errorf("command %q conflicts with alias for %q", cmd.Name, target)
	}

	for _, alias := range cmd.Aliases {
		if r.builtins[alias] && !cmd.Builtin {
			return fmt.Errorf("alias %q conflicts with builtin", alias)
		}
		if existing, ok := r.commands[alias]; ok {
			if existing.Builtin && !cmd.Builtin {
				return fmt.Errorf("alias %q conflicts with builtin command", alias)
			}
			return fmt.Errorf("alias %q conflicts with command %q", alias, existing.Name)
		}
		if target, ok := r.aliases[alias]; ok {
			if r.builtins[target] && !cmd.Builtin {
				return fmt.Errorf("alias %q conflicts with builtin alias", alias)
			}
			return fmt.Errorf("alias %q already points to command %q", alias, target)
		}
	}

	r.commands[cmd.Name] = cmd
	if cmd.Builtin {
		r.builtins[cmd.Name] = true
	}
	for _, alias := range cmd.Aliases {
		r.aliases[alias] = cmd.Name
		if cmd.Builtin {
			r.builtins[alias] = true
		}
	}
	return nil
}

func (r *Registry) RegisterWithWarning(cmd Command) {
	if err := r.Register(cmd); err != nil {
		r.AddWarning(err.Error())
	}
}

func (r *Registry) Find(name string) (Command, bool) {
	if r == nil {
		return Command{}, false
	}
	key := normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cmd, ok := r.commands[key]; ok {
		return cmd, true
	}
	if canonical, ok := r.aliases[key]; ok {
		cmd, ok := r.commands[canonical]
		return cmd, ok
	}
	return Command{}, false
}

func (r *Registry) HasConflict(name string) bool {
	if r == nil {
		return false
	}
	key := normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.builtins[key] {
		return true
	}
	if target, ok := r.aliases[key]; ok && r.builtins[target] {
		return true
	}
	return false
}

func (r *Registry) List() []CommandInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CommandInfo, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Complete(prefix string, limit int) []CommandInfo {
	if r == nil {
		return nil
	}
	prefix = normalizeName(prefix)
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := map[string]bool{}
	var out []CommandInfo
	add := func(name string) {
		if seen[name] {
			return
		}
		cmd, ok := r.commands[name]
		if !ok {
			return
		}
		seen[name] = true
		out = append(out, cmd.Info())
	}

	for name := range r.commands {
		if prefix == "" || strings.HasPrefix(name, prefix) {
			add(name)
		}
	}
	for alias, canonical := range r.aliases {
		if prefix == "" || strings.HasPrefix(alias, prefix) {
			add(canonical)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (r *Registry) AddWarning(warning string) {
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, warning)
}

func (r *Registry) Warnings() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.warnings...)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
