// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

func message(t *testing.T, raw string) *anthropic.Message {
	t.Helper()
	var m anthropic.Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestToMessages(t *testing.T) {
	msgs := toMessages([]Turn{{"user", "hola"}, {"assistant", "buenas"}}, "qué hora es")
	if len(msgs) != 3 || msgs[0].OfUser == nil || msgs[1].OfAssistant == nil || msgs[2].OfUser == nil {
		t.Fatalf("unexpected messages %+v", msgs)
	}
	if got := msgs[2].OfUser.Content[0].OfText.Text; got != "qué hora es" {
		t.Fatalf("question = %q", got)
	}
}

func TestAnswerText(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"drops search preamble and markdown": {
			`{"stop_reason":"end_turn","content":[
				{"type":"text","text":"Voy a buscar eso."},
				{"type":"server_tool_use","id":"s1","name":"web_search","input":{"query":"capital"}},
				{"type":"web_search_tool_result","tool_use_id":"s1","content":[]},
				{"type":"text","text":"La capital es **Lima**, en mi_país."}]}`,
			"La capital es Lima, en mi país.",
		},
		"refusal": {`{"stop_reason":"refusal","content":[]}`, "Lo siento, señor, no puedo ayudarle con eso."},
		"empty text": {
			`{"stop_reason":"end_turn","content":[{"type":"text","text":"  "}]}`,
			"Disculpe, no tengo respuesta para eso.",
		},
	}
	for name, c := range cases {
		if got := answerText(message(t, c.raw)); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestRunToolsReportsWeatherError(t *testing.T) {
	old := defaultCity
	defaultCity = "" // getWeather fails before any HTTP call
	t.Cleanup(func() { defaultCity = old })

	resp := message(t, `{"stop_reason":"tool_use","content":[
		{"type":"text","text":"Reviso el clima."},
		{"type":"tool_use","id":"tu1","name":"get_weather","input":{}}]}`)
	results := runTools(context.Background(), resp)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0].OfToolResult
	if r == nil || r.ToolUseID != "tu1" || !r.IsError.Value {
		t.Fatalf("unexpected result %+v", results[0])
	}
	raw, _ := json.Marshal(r)
	if !strings.Contains(string(raw), "ciudad") {
		t.Fatalf("error text missing: %s", raw)
	}
}

// fakeClaude serves canned Messages API responses in order (the last one
// repeats) and records each request body.
func fakeClaude(t *testing.T, responses ...string) (anthropic.Client, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[min(len(bodies), len(responses))-1]))
	}))
	t.Cleanup(srv.Close)
	return anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)), &bodies
}

const (
	toolUseResp = `{"id":"m1","type":"message","role":"assistant","model":"m","stop_reason":"tool_use",
		"content":[{"type":"tool_use","id":"tu1","name":"get_weather","input":{}}],"usage":{}}`
	finalResp = `{"id":"m2","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
		"content":[{"type":"text","text":"Mañana llueve."}],"usage":{}}`
)

func TestRunToolLoopSendsToolResult(t *testing.T) {
	old := defaultCity
	defaultCity = ""
	t.Cleanup(func() { defaultCity = old })

	c, bodies := fakeClaude(t, toolUseResp, finalResp)
	resp, err := runToolLoop(t.Context(), c, toMessages(nil, "va a llover"), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := answerText(resp); got != "Mañana llueve." {
		t.Fatalf("answer = %q", got)
	}
	if len(*bodies) != 2 || !strings.Contains((*bodies)[1], `"tool_use_id":"tu1"`) {
		t.Fatalf("second request must carry the tool result; got %d requests: %v", len(*bodies), *bodies)
	}
}

func TestRunToolLoopStopsAtMaxCalls(t *testing.T) {
	old := defaultCity
	defaultCity = ""
	t.Cleanup(func() { defaultCity = old })

	c, bodies := fakeClaude(t, toolUseResp) // Claude never stops asking
	if _, err := runToolLoop(t.Context(), c, toMessages(nil, "clima"), ""); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != maxClaudeCalls {
		t.Fatalf("made %d calls, want %d", len(*bodies), maxClaudeCalls)
	}
}
