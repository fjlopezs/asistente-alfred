// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

const testSkill = "amzn1.ask.skill.test"

// event builds an Alexa request from JSON so tests exercise the real wire shape.
func event(t *testing.T, reqType, intent string, slots map[string]string, history []Turn) Request {
	t.Helper()
	s := map[string]any{}
	for k, v := range slots {
		s[k] = map[string]string{"name": k, "value": v}
	}
	raw, _ := json.Marshal(map[string]any{
		"session": map[string]any{
			"sessionId":   "sess-1",
			"user":        map[string]string{"userId": "user-1"},
			"application": map[string]string{"applicationId": testSkill},
			"attributes":  map[string]any{"history": history},
		},
		"request": map[string]any{
			"type":      reqType,
			"requestId": "req-1",
			"intent":    map[string]any{"name": intent, "slots": s},
		},
	})
	var ev Request
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	return ev
}

var tags = regexp.MustCompile(`<[^>]+>`)

// said returns the plain text Alexa would speak.
func said(r Response) string {
	if r.Response.OutputSpeech == nil {
		return ""
	}
	return html.UnescapeString(tags.ReplaceAllString(r.Response.OutputSpeech.SSML, ""))
}

// withClaude makes getClient return a fake API; it returns the request bodies.
func withClaude(t *testing.T, responses ...string) *[]string {
	t.Helper()
	c, bodies := fakeClaude(t, responses...)
	claudeClient.set(c)
	t.Cleanup(func() { claudeClient = lazy[anthropic.Client]{} })
	return bodies
}

func withSkillID(t *testing.T) {
	t.Helper()
	old := skillID
	skillID = testSkill
	t.Cleanup(func() { skillID = old })
}

func answer(text string) string {
	b, _ := json.Marshal(text)
	return `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
		"content":[{"type":"text","text":` + string(b) + `}],"usage":{}}`
}

func run(t *testing.T, ev Request) Response {
	t.Helper()
	r, err := handler(t.Context(), ev)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHandlerFixedReplies(t *testing.T) {
	withSkillID(t)
	cases := []struct {
		name, reqType, intent string
		slots                 map[string]string
		want                  string
		end                   bool
	}{
		{"launch", "LaunchRequest", "", nil, "A sus órdenes. ¿En qué puedo ayudarle?", false},
		{"stop", "IntentRequest", "AMAZON.StopIntent", nil, "Hasta luego, señor.", true},
		{"cancel", "IntentRequest", "AMAZON.CancelIntent", nil, "Hasta luego, señor.", true},
		{"help", "IntentRequest", "AMAZON.HelpIntent", nil,
			"Hágame cualquier pregunta, por ejemplo: pregunta cuál es la capital de Australia.", false},
		{"fallback", "IntentRequest", "AMAZON.FallbackIntent", nil,
			"No le entendí. Empiece con: pregunta, o dime, seguido de su duda.", false},
		{"bare carrier", "IntentRequest", "AskIntent", nil, "Dígame, señor.", false},
		{"empty chat", "IntentRequest", "ChatIntent", map[string]string{"query": ""}, "¿Qué desea preguntarme?", false},
		{"empty question word", "IntentRequest", "CuandoIntent", nil, "¿Qué desea preguntarme?", false},
		{"unknown intent", "IntentRequest", "NoSuchIntent", nil, "No entendí eso.", false},
		{"unknown request", "Display.ElementSelected", "", nil, "No entendí eso.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := run(t, event(t, c.reqType, c.intent, c.slots, nil))
			if got := said(r); got != c.want {
				t.Errorf("said %q, want %q", got, c.want)
			}
			if *r.Response.ShouldEndSession != c.end {
				t.Errorf("shouldEndSession = %v, want %v", *r.Response.ShouldEndSession, c.end)
			}
			if !c.end && r.Response.Reprompt == nil {
				t.Error("open session needs a reprompt")
			}
		})
	}
}

func TestHandlerSessionEndedIsSilent(t *testing.T) {
	r := run(t, event(t, "SessionEndedRequest", "", nil, nil))
	if r.Response.OutputSpeech != nil {
		t.Fatalf("SessionEndedRequest must not speak: %+v", r.Response)
	}
}

func TestHandlerRejectsOtherSkills(t *testing.T) {
	withSkillID(t)
	ev := event(t, "LaunchRequest", "", nil, nil)
	ev.Session.Application.ApplicationID = "amzn1.ask.skill.other"
	if _, err := handler(t.Context(), ev); err == nil {
		t.Fatal("expected request from another skill to be rejected")
	}
}

// Alexa strips the question word from SearchQuery slots; Claude must get it back.
func TestHandlerRestoresQuestionWord(t *testing.T) {
	bodies := withClaude(t, answer("En 1783."))
	r := run(t, event(t, "IntentRequest", "CuandoIntent", map[string]string{"query": "nació Bolívar"}, nil))
	if said(r) != "En 1783." {
		t.Fatalf("said %q", said(r))
	}
	if !strings.Contains((*bodies)[0], `"cuándo nació Bolívar"`) {
		t.Fatalf("question word not restored: %s", (*bodies)[0])
	}
	h := r.SessionAttributes.History
	if len(h) != 2 || h[0] != (Turn{"user", "cuándo nació Bolívar"}) || h[1] != (Turn{"assistant", "En 1783."}) {
		t.Fatalf("history = %+v", h)
	}
}

func TestHandlerChatAndWeatherQuestions(t *testing.T) {
	cases := []struct {
		intent string
		slots  map[string]string
		want   string
	}{
		{"ChatIntent", map[string]string{"query": "la miel es contable"}, `"la miel es contable"`},
		{"WeatherIntent", map[string]string{"city": "Valdivia", "day": "mañana"},
			`"¿Cómo está el clima en Valdivia mañana? ¿Va a llover?"`},
	}
	for _, c := range cases {
		bodies := withClaude(t, answer("ok"))
		run(t, event(t, "IntentRequest", c.intent, c.slots, nil))
		if !strings.Contains((*bodies)[0], c.want) {
			t.Errorf("%s: request lacks %s: %s", c.intent, c.want, (*bodies)[0])
		}
	}
}

func TestHandlerSendsAndTrimsHistory(t *testing.T) {
	var history []Turn
	for i := range maxTurns {
		history = append(history, Turn{"user", "pregunta " + string(rune('a'+i))}, Turn{"assistant", "respuesta"})
	}
	bodies := withClaude(t, answer("nueva respuesta"))
	r := run(t, event(t, "IntentRequest", "ChatIntent", map[string]string{"query": "otra cosa"}, history))

	if !strings.Contains((*bodies)[0], `"pregunta a"`) {
		t.Fatal("session history not sent to Claude")
	}
	h := r.SessionAttributes.History
	if len(h) != 2*maxTurns {
		t.Fatalf("history has %d turns, want %d", len(h), 2*maxTurns)
	}
	if h[0].Content != "pregunta b" || h[len(h)-1].Content != "nueva respuesta" {
		t.Fatalf("oldest pair not dropped: first=%q last=%q", h[0].Content, h[len(h)-1].Content)
	}
}

func TestHandlerUsesAndSavesMemory(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).UnixMilli()
	db := &fakeDDB{items: []memItem{
		{User: "user-1", TS: past, Session: "old", Question: "río más largo", Answer: "El Loa."},
	}}
	withFakeDDB(t, db)
	bodies := withClaude(t, answer("Le pregunté por el río más largo."))

	run(t, event(t, "IntentRequest", "ChatIntent", map[string]string{"query": "qué te pregunté ayer"}, nil))

	if !strings.Contains((*bodies)[0], "P: río más largo | R: El Loa.") {
		t.Fatalf("memory missing from system prompt: %s", (*bodies)[0])
	}
	var saved memItem
	if err := attributevalue.UnmarshalMap(db.put, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.User != "user-1" || saved.Session != "sess-1" || saved.Question != "qué te pregunté ayer" ||
		saved.Answer != "Le pregunté por el río más largo." {
		t.Fatalf("saved %+v", saved)
	}
}

func TestHandlerMapsClaudeErrors(t *testing.T) {
	apiError := func(status int) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"boom"}}`))
		}))
		t.Cleanup(srv.Close)
		claudeClient.set(anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("k"), option.WithMaxRetries(0)))
	}
	t.Cleanup(func() { claudeClient = lazy[anthropic.Client]{} })
	ask := func(ctx context.Context) string {
		r, err := handler(ctx, event(t, "IntentRequest", "ChatIntent", map[string]string{"query": "hola"}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if r.Response.ShouldEndSession == nil || *r.Response.ShouldEndSession {
			t.Fatal("errors must keep the session open")
		}
		return said(r)
	}

	apiError(http.StatusTooManyRequests)
	if got := ask(t.Context()); got != "Estoy recibiendo demasiadas peticiones. Inténtelo en un momento." {
		t.Errorf("429: %q", got)
	}
	apiError(http.StatusInternalServerError)
	if got := ask(t.Context()); got != "Hubo un problema al consultar a Claude." {
		t.Errorf("500: %q", got)
	}

	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if got := ask(expired); got != "Disculpe, tardé demasiado pensando. ¿Puede repetir la pregunta de forma más corta?" {
		t.Errorf("timeout: %q", got)
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused
	claudeClient.set(anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("k"), option.WithMaxRetries(0)))
	if got := ask(t.Context()); got != "No pude conectarme con Claude." {
		t.Errorf("network: %q", got)
	}
}
