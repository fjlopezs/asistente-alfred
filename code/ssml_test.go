// SPDX-License-Identifier: MIT

package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestSSMLEscapesClaudeText(t *testing.T) {
	s := ssml(`AT&T dice "3 < 5" y 'ok'`)
	if err := xml.Unmarshal([]byte(s), new(struct{})); err != nil {
		t.Fatalf("invalid SSML %q: %v", s, err)
	}
	if !strings.Contains(s, `<voice name="Miguel">`) || !strings.Contains(s, "AT&amp;T") {
		t.Fatalf("unexpected SSML %q", s)
	}
}
