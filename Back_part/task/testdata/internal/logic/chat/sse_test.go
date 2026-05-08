package chat

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSSEPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "single line",
			data: "hello",
			want: "data: hello\n\n",
		},
		{
			name: "mixed newlines",
			data: "hello\r\nworld\rnext",
			want: "data: hello\ndata: world\ndata: next\n\n",
		},
		{
			name: "trailing newline preserved",
			data: "hello\n",
			want: "data: hello\ndata: \n\n",
		},
		{
			name: "empty payload",
			data: "",
			want: "data: \n\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := writeSSEPayload(&buf, tt.data); err != nil {
				t.Fatalf("writeSSEPayload returned error: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("unexpected SSE payload\nwant: %q\ngot:  %q", tt.want, got)
			}
		})
	}
}

func TestWriteSSEPayloadLargeLine(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("a", 70*1024)
	var buf bytes.Buffer
	if err := writeSSEPayload(&buf, data); err != nil {
		t.Fatalf("writeSSEPayload returned error: %v", err)
	}

	want := "data: " + data + "\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("unexpected SSE payload size=%d want=%d", len(got), len(want))
	}
}
