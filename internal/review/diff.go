package review

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Hunk is one changed region between the baseline and the current file.
type Hunk struct {
	ID       string
	OldStart int
	NewStart int
	Old      []string
	New      []string
}

// Diff returns changed regions. Line anchors stay on the baseline, so a later
// reject does not rename the hunks that remain.
func Diff(baseline, current string) []Hunk {
	oldLines := splitLines(baseline)
	newLines := splitLines(current)
	matches := lcs(oldLines, newLines)
	var hunks []Hunk
	var oldBuf, newBuf []string
	oldStart, newStart := 0, 0
	oi, ni := 0, 0
	flush := func() {
		if len(oldBuf) == 0 && len(newBuf) == 0 {
			return
		}
		hunks = append(hunks, Hunk{
			ID:       hunkID(oldStart, oldBuf, newBuf),
			OldStart: oldStart,
			NewStart: newStart,
			Old:      append([]string(nil), oldBuf...),
			New:      append([]string(nil), newBuf...),
		})
		oldBuf, newBuf = nil, nil
	}
	for _, match := range matches {
		for oi < match[0] || ni < match[1] {
			if len(oldBuf) == 0 && len(newBuf) == 0 {
				oldStart = oi
				newStart = ni
			}
			if oi < match[0] {
				oldBuf = append(oldBuf, oldLines[oi])
				oi++
			}
			if ni < match[1] {
				newBuf = append(newBuf, newLines[ni])
				ni++
			}
		}
		flush()
		oi++
		ni++
	}
	if oi < len(oldLines) || ni < len(newLines) {
		if len(oldBuf) == 0 && len(newBuf) == 0 {
			oldStart = oi
			newStart = ni
		}
		for oi < len(oldLines) {
			oldBuf = append(oldBuf, oldLines[oi])
			oi++
		}
		for ni < len(newLines) {
			newBuf = append(newBuf, newLines[ni])
			ni++
		}
		flush()
	}
	return hunks
}

// Reject replaces the hunk's current lines with the baseline lines.
func Reject(current string, hunk Hunk) (string, error) {
	lines := splitLines(current)
	if hunk.NewStart < 0 || hunk.NewStart+len(hunk.New) > len(lines) {
		return "", fmt.Errorf("hunk no longer matches")
	}
	for i, line := range hunk.New {
		if lines[hunk.NewStart+i] != line {
			return "", fmt.Errorf("hunk no longer matches")
		}
	}
	next := make([]string, 0, len(lines)-len(hunk.New)+len(hunk.Old))
	next = append(next, lines[:hunk.NewStart]...)
	next = append(next, hunk.Old...)
	next = append(next, lines[hunk.NewStart+len(hunk.New):]...)
	return joinLines(next), nil
}

func hunkID(oldStart int, oldLines, newLines []string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s", oldStart, strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"))))
	return hex.EncodeToString(sum[:8])
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func lcs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var matches [][2]int
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			matches = append(matches, [2]int{i, j})
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return matches
}
