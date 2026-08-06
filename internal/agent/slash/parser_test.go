package slash

import "testing"

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		wantOK bool
		want   ParsedCommand
	}{
		{name: "plain text", input: "hello", wantOK: false},
		{name: "empty slash", input: "/", wantOK: true, want: ParsedCommand{Raw: "/"}},
		{name: "command only", input: "/help", wantOK: true, want: ParsedCommand{Name: "help", Raw: "/help"}},
		{name: "trims args", input: "  /k8s   pods -n prod  ", wantOK: true, want: ParsedCommand{Name: "k8s", Args: "pods -n prod", Raw: "/k8s   pods -n prod"}},
		{name: "normalizes command", input: "/K8S pods", wantOK: true, want: ParsedCommand{Name: "k8s", Args: "pods", Raw: "/K8S pods"}},
		{name: "colon command", input: "/git:log --oneline", wantOK: true, want: ParsedCommand{Name: "git:log", Args: "--oneline", Raw: "/git:log --oneline"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Parse(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("Parse ok=%v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Fatalf("Parse=%#v, want %#v", got, tt.want)
			}
		})
	}
}
