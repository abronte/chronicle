package internal

import (
	"fmt"
	"os"
	"strings"
)

func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func IsAscii(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return false, nil
	}

	for _, b := range data {
		if b >= 128 {
			return false, nil
		}
		if b == 0 {
			return false, nil
		}
	}

	return true, nil
}

func CreateDiff(a, b string) string {
	aLines := splitLines(a)
	bLines := splitLines(b)

	lcs := lcsTable(aLines, bLines)
	var chunks []diffHunk
	i, j := len(aLines), len(bLines)
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && aLines[i-1] == bLines[j-1] {
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			chunks = append(chunks, diffHunk{add: true, line: bLines[j-1]})
			j--
		} else if i > 0 {
			chunks = append(chunks, diffHunk{add: false, line: aLines[i-1]})
			i--
		}
	}

	for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
		chunks[i], chunks[j] = chunks[j], chunks[i]
	}

	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder

	hunks := groupHunks(chunks)
	oldStart, newStart := 1, 1
	for _, h := range hunks {
		oldCount := countLines(h, false)
		newCount := countLines(h, true)
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))
		for _, d := range h {
			if d.add {
				sb.WriteString("+")
			} else {
				sb.WriteString("-")
			}
			sb.WriteString(d.line)
			sb.WriteString("\n")
		}
		oldStart += oldCount
		newStart += newCount
	}

	return sb.String()
}

type diffHunk struct {
	add  bool
	line string
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	table := make([][]int, m+1)
	for i := range table {
		table[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
			} else {
				table[i][j] = table[i-1][j]
				if table[i][j-1] > table[i][j] {
					table[i][j] = table[i][j-1]
				}
			}
		}
	}
	return table
}

func groupHunks(chunks []diffHunk) [][]diffHunk {
	var result [][]diffHunk
	start := 0
	for i := 1; i <= len(chunks); i++ {
		if i == len(chunks) || (chunks[i-1].add && !chunks[i].add) {
			isNewHunk := i == len(chunks)
			if !isNewHunk {
				del := 0
				ins := 0
				for j := start; j < i; j++ {
					if chunks[j].add {
						ins++
					} else {
						del++
					}
				}
				if del > 0 && ins == 0 {
					continue
				}
			}
			result = append(result, chunks[start:i])
			start = i
		}
	}
	if start < len(chunks) {
		result = append(result, chunks[start:])
	}
	return result
}

func countLines(hunk []diffHunk, add bool) int {
	count := 0
	for _, d := range hunk {
		if d.add == add {
			count++
		}
	}
	return count
}
