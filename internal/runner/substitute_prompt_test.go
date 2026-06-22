package runner

import (
	"slices"
	"testing"
)

func TestSubstitutePrompt(t *testing.T) {
	tests := []struct {
		name   string
		agent  []string
		prompt string
		want   []string
	}{
		{
			name:   "replaces the token in place",
			agent:  []string{"/bin/echo", "out:", "{prompt}"},
			prompt: "hello",
			want:   []string{"/bin/echo", "out:", "hello"},
		},
		{
			name:   "appends the prompt when the token is absent",
			agent:  []string{"/bin/echo", "out:"},
			prompt: "hello",
			want:   []string{"/bin/echo", "out:", "hello"},
		},
		{
			name:   "replaces every token occurrence",
			agent:  []string{"{prompt}", "--", "{prompt}"},
			prompt: "hello",
			want:   []string{"hello", "--", "hello"},
		},
		{
			name:   "leaves an embedded token untouched and appends",
			agent:  []string{"--flag={prompt}"},
			prompt: "hello",
			want:   []string{"--flag={prompt}", "hello"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substitutePrompt(tt.agent, tt.prompt)
			if !slices.Equal(got, tt.want) {
				t.Errorf("substitutePrompt(%q, %q) = %q, want %q", tt.agent, tt.prompt, got, tt.want)
			}
		})
	}
}
