package tui

import "testing"

func TestClassifyResponse(t *testing.T) {
	tests := []struct {
		input    string
		expected responseClass
	}{
		// Empty (retry)
		{"", responseEmpty},

		// Simple responses
		{"next", responseSimple},
		{"Next.", responseSimple},
		{"LOOKS GOOD", responseSimple},
		{"looks good.", responseSimple},
		{"continue", responseSimple},
		{"Continue.", responseSimple},
		{"lgtm", responseSimple},
		{"ok", responseSimple},
		{"Okay.", responseSimple},
		{"yes", responseSimple},
		{"go ahead", responseSimple},
		{"   next   ", responseSimple},
		{"looks good to me", responseSimple},
		{"Approved.", responseSimple},

		// Skip
		{"skip", responseSkip},
		{"Skip.", responseSkip},
		{"pass", responseSkip},

		// Stop
		{"stop", responseStop},
		{"Stop.", responseStop},
		{"done", responseStop},
		{"finish", responseStop},
		{"that's all", responseStop},

		// Complex — should NOT match simple
		{"fix the null check on line 42", responseComplex},
		{"looks good but also check line 10", responseComplex},
		{"there is a bug online", responseComplex},
		{"can you fix that", responseComplex},
		{"I think we need to refactor this", responseComplex},
		{"what does this function do", responseComplex},
		{"next time use a different pattern", responseComplex},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := classifyResponse(tt.input)
			if got != tt.expected {
				t.Errorf("classifyResponse(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
