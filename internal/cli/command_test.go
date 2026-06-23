package cli

import "testing"

func TestRenderCommand(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "no quoting needed",
			argv: []string{"claude", "-p", "--output-format=stream-json"},
			want: "claude -p --output-format=stream-json",
		},
		{
			name: "prompt with spaces is quoted",
			argv: []string{"claude", "-p", "review the diff", "--output-format", "stream-json"},
			want: "claude -p 'review the diff' --output-format stream-json",
		},
		{
			name: "embedded single quote is escaped",
			argv: []string{"echo", "it's done"},
			want: `echo 'it'\''s done'`,
		},
		{
			name: "bare single quote",
			argv: []string{"echo", "'"},
			want: `echo ''\'''`,
		},
		{
			name: "shell metacharacters are quoted",
			argv: []string{"sh", "-c", "echo $HOME | wc"},
			want: `sh -c 'echo $HOME | wc'`,
		},
		{
			name: "empty argument",
			argv: []string{"echo", ""},
			want: "echo ''",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderCommand(tt.argv); got != tt.want {
				t.Errorf("renderCommand(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}
