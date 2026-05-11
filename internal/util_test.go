package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAscii(t *testing.T) {
	dir := t.TempDir()

	writeFile := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("plain ascii text", func(t *testing.T) {
		p := writeFile("ascii.txt", []byte("hello world\n"))
		ok, err := IsAscii(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true for ascii text")
		}
	})

	t.Run("binary file with zero byte", func(t *testing.T) {
		p := writeFile("binary.bin", []byte{0x00, 0x41, 0x42})
		ok, err := IsAscii(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected false for binary with zero byte")
		}
	})

	t.Run("binary file with high byte", func(t *testing.T) {
		p := writeFile("high.bin", []byte{0x80, 0x41, 0x42})
		ok, err := IsAscii(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected false for binary with byte >= 128")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		p := writeFile("empty.txt", []byte{})
		ok, err := IsAscii(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected false for empty file")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := IsAscii(filepath.Join(dir, "nope.txt"))
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	t.Run("ascii symbols and numbers", func(t *testing.T) {
		p := writeFile("sym.txt", []byte("123 !@#$%^&*(){}[]<>?"))
		ok, err := IsAscii(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Error("expected true for ascii symbols")
		}
	})
}

func TestGetFileSize(t *testing.T) {
	dir := t.TempDir()

	t.Run("regular file", func(t *testing.T) {
		p := filepath.Join(dir, "f.txt")
		if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		size, err := GetFileSize(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if size != 5 {
			t.Errorf("expected size 5, got %d", size)
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := GetFileSize(filepath.Join(dir, "nope.txt"))
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty string", "", nil},
		{"single line no trailing newline", "hello", []string{"hello"}},
		{"two lines", "hello\nworld", []string{"hello", "world"}},
		{"trailing newline", "hello\n", []string{"hello", ""}},
		{"windows line endings", "a\r\nb", []string{"a\r", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d lines, got %d: %#v", len(tt.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("line %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

func TestLcsTable(t *testing.T) {
	t.Run("identical lines", func(t *testing.T) {
		table := lcsTable([]string{"a", "b", "c"}, []string{"a", "b", "c"})
		if table[3][3] != 3 {
			t.Errorf("expected LCS length 3, got %d", table[3][3])
		}
	})

	t.Run("no common lines", func(t *testing.T) {
		table := lcsTable([]string{"a", "b"}, []string{"c", "d"})
		if table[2][2] != 0 {
			t.Errorf("expected LCS length 0, got %d", table[2][2])
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		table := lcsTable([]string{"a", "b", "c", "d"}, []string{"b", "c", "e"})
		if table[4][3] != 2 {
			t.Errorf("expected LCS length 2, got %d", table[4][3])
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		table := lcsTable(nil, nil)
		if table[0][0] != 0 {
			t.Errorf("expected LCS length 0, got %d", table[0][0])
		}
	})
}

func TestGroupHunks(t *testing.T) {
	chunks := []diffHunk{
		{add: false, line: "old1"},
		{add: true, line: "new1"},
		{add: false, line: "old2"},
		{add: true, line: "new2"},
	}
	hunks := groupHunks(chunks)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
}

func TestCountLines(t *testing.T) {
	hunk := []diffHunk{
		{add: false, line: "a"},
		{add: true, line: "b"},
		{add: false, line: "c"},
	}
	if c := countLines(hunk, true); c != 1 {
		t.Errorf("expected 1 add, got %d", c)
	}
	if c := countLines(hunk, false); c != 2 {
		t.Errorf("expected 2 dels, got %d", c)
	}
}

func TestCreateDiff(t *testing.T) {
	t.Run("identical strings", func(t *testing.T) {
		diff := CreateDiff("hello\nworld", "hello\nworld")
		if diff != "" {
			t.Errorf("expected empty diff for identical strings, got:\n%s", diff)
		}
	})

	t.Run("single line added", func(t *testing.T) {
		diff := CreateDiff("line1", "line1\nline2")
		if diff == "" {
			t.Error("expected non-empty diff")
		}
		if !contains(diff, "+line2") {
			t.Errorf("expected +line2 in diff:\n%s", diff)
		}
	})

	t.Run("single line removed", func(t *testing.T) {
		diff := CreateDiff("line1\nline2", "line1")
		if diff == "" {
			t.Error("expected non-empty diff")
		}
		if !contains(diff, "-line2") {
			t.Errorf("expected -line2 in diff:\n%s", diff)
		}
	})

	t.Run("complete replacement", func(t *testing.T) {
		diff := CreateDiff("old", "new")
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("multiple changes", func(t *testing.T) {
		a := "line1\nline2\nline3\nline4"
		b := "line1\nline2-modified\nline3\nline5"
		diff := CreateDiff(a, b)
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("old empty", func(t *testing.T) {
		diff := CreateDiff("", "new content")
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})

	t.Run("new empty", func(t *testing.T) {
		diff := CreateDiff("old content", "")
		if diff == "" {
			t.Error("expected non-empty diff")
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
