package intent

import "testing"

func TestClassify_AllNine(t *testing.T) {
	c := DefaultClassifier()
	cases := []struct {
		input string
		want  Intent
	}{
		{"please fix this bug in the parser", CodeFix},
		{"can you refactor this code to be cleaner", CodeRefactor},
		{"write a function that adds two numbers", CodeGeneration},
		{"explain what this method does", CodeExplain},
		{"run this bash command and show output", ShellExec},
		{"save the result to /tmp/out.json", FileWrite},
		{"open the file /etc/hosts", FileRead},
		{"search for the term 'TODO' in this codebase", Search},
		{"hello, how are you today?", Chat},
	}
	for _, c := range cases {
		t.Run(string(c.want), func(t *testing.T) {
			got := c.input
			_ = got
		})
	}
	for _, tc := range cases {
		got := c.Classify(tc.input)
		if got != tc.want {
			t.Errorf("input=%q → got=%s, want=%s", tc.input, got, tc.want)
		}
	}
}

func TestClassify_Empty(t *testing.T) {
	if got := DefaultClassifier().Classify(""); got != Chat {
		t.Errorf("empty → %s, want chat", got)
	}
}

func TestClassify_FirstHitWins(t *testing.T) {
	// "fix this bug in the code" should match CodeFix before any later category
	got := DefaultClassifier().Classify("please fix this bug in the code")
	if got != CodeFix {
		t.Errorf("fix wins = %s, want code_fix", got)
	}
}

func TestAll(t *testing.T) {
	all := All()
	if len(all) != 9 {
		t.Errorf("All() len = %d", len(all))
	}
}
