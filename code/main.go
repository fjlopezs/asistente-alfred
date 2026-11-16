// Alfred: Alexa skill that answers with Claude.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const (
	maxTurns = 10 // user/assistant pairs kept in the Alexa session
	system   = "Eres Alfred, un mayordomo asistente de voz que responde a través de Alexa en español latinoamericano neutro. " +
		"Tus respuestas se leen en voz alta: sé muy conciso, una o dos frases cortas (unas 40 palabras como máximo). " +
		"Ve directo al dato: sin introducciones, sin repetir la pregunta y sin cerrar ofreciendo más ayuda. " +
		"Si el tema es amplio, di solo lo esencial; el usuario pedirá más si lo quiere. " +
		"No uses markdown, listas, viñetas, emojis, URLs ni símbolos que no se puedan pronunciar. " +
		"Usa web_search solo para información actual o que cambia (noticias, resultados deportivos, precios, " +
		"eventos, horarios); para conocimiento general responde directamente. " +
		"Di solo la respuesta: nunca hables de búsquedas, herramientas, fuentes ni URLs, ni expliques cómo obtuviste la información."
)

// webSearch is Claude's server-side search: one query per question keeps us
// inside Alexa's ~8s window.
var webSearch = anthropic.WebSearchTool20250305Param{
	MaxUses: anthropic.Int(1),
	UserLocation: anthropic.UserLocationParam{
		City:     anthropic.String("Temuco"),
		Region:   anthropic.String("Araucanía"),
		Country:  anthropic.String("CL"),
		Timezone: anthropic.String("America/Santiago"),
	},
}

var tools = []anthropic.ToolUnionParam{
	{OfTool: &weatherTool},
	{OfWebSearchTool20250305: &webSearch},
}

var (
	model       = envOr("CLAUDE_MODEL", "claude-haiku-5-5")
	skillID     = os.Getenv("ALEXA_SKILL_ID")
	apiKeyParam = envOr("API_KEY_PARAM", "/alfred/anthropic-api-key")

	claudeClient lazy[anthropic.Client]
	awsCfg       lazy[aws.Config]
)

func awsConfig(ctx context.Context) (aws.Config, error) {
	return awsCfg.get(ctx, func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx)
	})
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// --- Alexa request/response (minimal subset) ---

type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Attributes struct {
	History []Turn `json:"history,omitempty"`
}

type User struct {
	UserID string `json:"userId"`
}

type Request struct {
	Session struct {
		SessionID   string `json:"sessionId"`
		User        User   `json:"user"`
		Application struct {
			ApplicationID string `json:"applicationId"`
		} `json:"application"`
		Attributes Attributes `json:"attributes"`
	} `json:"session"`
	Context struct {
		System struct {
			Application struct {
				ApplicationID string `json:"applicationId"`
			} `json:"application"`
			User           User   `json:"user"`
			APIEndpoint    string `json:"apiEndpoint"`
			APIAccessToken string `json:"apiAccessToken"`
		} `json:"System"`
	} `json:"context"`
	Request struct {
		Type      string `json:"type"`
		RequestID string `json:"requestId"`
		Locale    string `json:"locale"`
		Reason    string `json:"reason"`
		Error     struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Intent struct {
			Name  string `json:"name"`
			Slots map[string]struct {
				Value string `json:"value"`
			} `json:"slots"`
		} `json:"intent"`
	} `json:"request"`
}

type Speech struct {
	Type string `json:"type"`
	SSML string `json:"ssml"`
}

// ssml wraps text in Alfred's voice: Polly "Miguel" (es-US male), deep and slow.
func ssml(text string) string {
	return `<speak><voice name="Miguel"><prosody pitch="x-low" rate="90%">` +
		html.EscapeString(text) + `</prosody></voice></speak>`
}

func voice(text string) Speech { return Speech{Type: "SSML", SSML: ssml(text)} }

type Reprompt struct {
	OutputSpeech Speech `json:"outputSpeech"`
}

type ResponseBody struct {
	OutputSpeech     *Speech   `json:"outputSpeech,omitempty"`
	Reprompt         *Reprompt `json:"reprompt,omitempty"`
	ShouldEndSession *bool     `json:"shouldEndSession,omitempty"`
}

type Response struct {
	Version           string       `json:"version"`
	SessionAttributes Attributes   `json:"sessionAttributes"`
	Response          ResponseBody `json:"response"`
}

func speak(text string, end bool, attrs Attributes) Response {
	r := Response{Version: "1.0", SessionAttributes: attrs, Response: ResponseBody{
		OutputSpeech:     ptr(voice(text)),
		ShouldEndSession: aws.Bool(end),
	}}
	if !end {
		r.Response.Reprompt = &Reprompt{OutputSpeech: voice("¿Algo más?")}
	}
	return r
}

func ptr[T any](v T) *T { return &v }

// --- Claude ---

func getClient(ctx context.Context) (anthropic.Client, error) {
	return claudeClient.get(ctx, func(ctx context.Context) (anthropic.Client, error) {
		cfg, err := awsConfig(ctx)
		if err != nil {
			return anthropic.Client{}, err
		}
		out, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{
			Name: aws.String(apiKeyParam), WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return anthropic.Client{}, err
		}
		// Alexa waits ~8s for the skill; fail fast instead of retrying.
		return anthropic.NewClient(
			option.WithAPIKey(*out.Parameter.Value),
			option.WithRequestTimeout(7*time.Second),
			option.WithMaxRetries(0),
		), nil
	})
}

func systemPrompt(memory string) string {
	s := system + "\nFecha y hora UTC actual: " + time.Now().UTC().Format(time.RFC3339) + "."
	if defaultCity != "" {
		s += "\nEl usuario vive en " + defaultCity + ". Si pregunta por el clima sin decir el lugar, " +
			"usa get_weather sin city (su ciudad) sin preguntarle."
	}
	return s + memory
}

const maxClaudeCalls = 3 // tool round trips must fit in Alexa's ~8s window

func askClaude(ctx context.Context, question string, history []Turn, memory string) (string, error) {
	c, err := getClient(ctx)
	if err != nil {
		return "", fmt.Errorf("init client: %w", err)
	}
	// One deadline for the whole tool loop: Alexa gives up after ~8s.
	ctx, cancel := context.WithTimeout(ctx, 7500*time.Millisecond)
	defer cancel()
	resp, err := runToolLoop(ctx, c, toMessages(history, question), memory)
	if err != nil {
		return "", err
	}
	return answerText(resp), nil
}

func toMessages(history []Turn, question string) []anthropic.MessageParam {
	msgs := make([]anthropic.MessageParam, 0, len(history)+1)
	for _, t := range history {
		if t.Role == "assistant" {
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(t.Content)))
		} else {
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(t.Content)))
		}
	}
	return append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(question)))
}

// runToolLoop calls Claude until it stops asking for tools, resuming paused
// server-side searches, for at most maxClaudeCalls requests.
func runToolLoop(
	ctx context.Context, c anthropic.Client, msgs []anthropic.MessageParam, memory string,
) (*anthropic.Message, error) {
	params := anthropic.MessageNewParams{
		Model:        anthropic.Model(model),
		MaxTokens:    250,
		System:       []anthropic.TextBlockParam{{Text: systemPrompt(memory)}},
		Thinking:     anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}},
		OutputConfig: anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffortLow},
		Tools:        tools,
		Messages:     msgs,
	}
	for i := 1; ; i++ {
		resp, err := c.Messages.New(ctx, params)
		if err != nil || i == maxClaudeCalls {
			return resp, err
		}
		switch resp.StopReason {
		case anthropic.StopReasonPauseTurn:
			// Server-side search paused; resend as-is and the API resumes.
			params.Messages = append(params.Messages, resp.ToParam())
		case anthropic.StopReasonToolUse:
			params.Messages = append(params.Messages, resp.ToParam(), anthropic.NewUserMessage(runTools(ctx, resp)...))
		default:
			return resp, nil
		}
	}
}

// runTools executes the client-side tool calls in resp (only get_weather).
func runTools(ctx context.Context, resp *anthropic.Message) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion
	for _, b := range resp.Content {
		if tu, ok := b.AsAny().(anthropic.ToolUseBlock); ok {
			results = append(results, weatherResult(ctx, tu))
		}
	}
	return results
}

func weatherResult(ctx context.Context, tu anthropic.ToolUseBlock) anthropic.ContentBlockParamUnion {
	var in struct {
		City string `json:"city"`
	}
	_ = json.Unmarshal([]byte(tu.JSON.Input.Raw()), &in)
	out, err := getWeather(ctx, in.City)
	log.Printf("tool %s city=%q err=%v", tu.Name, in.City, err)
	if err != nil {
		return anthropic.NewToolResultBlock(tu.ID, err.Error(), true)
	}
	return anthropic.NewToolResultBlock(tu.ID, out, false)
}

// markdown strips characters Alexa would otherwise read or mangle.
var markdown = strings.NewReplacer("*", "", "#", "", "`", "", "_", " ")

// answerText turns Claude's final message into speakable text.
func answerText(resp *anthropic.Message) string {
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "Lo siento, señor, no puedo ayudarle con eso."
	}
	var sb strings.Builder
	for _, b := range resp.Content {
		switch v := b.AsAny().(type) {
		case anthropic.ServerToolUseBlock:
			log.Printf("web_search %s", v.JSON.Input.Raw())
		case anthropic.WebSearchToolResultBlock:
			sb.Reset() // drop preamble like "Voy a buscar..."
		case anthropic.TextBlock:
			sb.WriteString(v.Text)
		default:
		}
	}
	if s := strings.TrimSpace(markdown.Replace(sb.String())); s != "" {
		return s
	}
	return "Disculpe, no tengo respuesta para eso."
}

// questionIntents maps each question-word intent in the interaction model to
// the carrier phrase Alexa removes from the slot value.
var questionIntents = map[string]string{
	"QueEsIntent":     "qué es",
	"QueSonIntent":    "qué son",
	"QuienEsIntent":   "quién es",
	"QuienFueIntent":  "quién fue",
	"ComoIntent":      "cómo",
	"PorQueIntent":    "por qué",
	"CualEsIntent":    "cuál es",
	"CualesSonIntent": "cuáles son",
	"CuantoIntent":    "cuánto",
	"CuantosIntent":   "cuántos",
	"CuantasIntent":   "cuántas",
	"DondeIntent":     "dónde",
	"CuandoIntent":    "cuándo",
	// Broader openers so unmatched questions reach Claude instead of the
	// closest-sounding intent. Overlaps (es / es cierto que) are harmless:
	// restoring the carrier yields the original utterance either way.
	"QueIntent":        "qué",
	"EsIntent":         "es",
	"HayIntent":        "hay",
	"PuedesIntent":     "puedes",
	"ParaQueIntent":    "para qué",
	"AQueIntent":       "a qué",
	"EnQueIntent":      "en qué",
	"DeQueIntent":      "de qué",
	"ElIntent":         "el",
	"LaIntent":         "la",
	"LosIntent":        "los",
	"LasIntent":        "las",
	"EnIntent":         "en",
	"EsCiertoIntent":   "es cierto que",
	"EsVerdadIntent":   "es verdad que",
	"OcurreQueIntent":  "ocurre que",
	"ComoSeDiceIntent": "cómo se dice",
	"TraduceIntent":    "traduce",
}

// --- Progressive response ---

var thinkingPhrases = []string{
	"Déjame pensarlo...",
	"Dame un minuto...",
	"Enseguida te tengo la respuesta...",
	"A ver qué encuentro sobre esto...",
}

var httpClient = &http.Client{Timeout: 2 * time.Second}

// sendProgressive plays a filler phrase while Claude answers. It must reach
// Alexa before the final response, so callers wait on the returned channel.
func sendProgressive(ctx context.Context, ev Request) <-chan struct{} {
	done := make(chan struct{})
	sys := ev.Context.System
	if sys.APIEndpoint == "" || sys.APIAccessToken == "" || ev.Request.RequestID == "" {
		close(done) // e.g. direct test invocations
		return done
	}
	phrase := thinkingPhrases[rand.IntN(len(thinkingPhrases))]
	go func() {
		defer close(done)
		body, _ := json.Marshal(map[string]any{
			"header":    map[string]string{"requestId": ev.Request.RequestID},
			"directive": map[string]string{"type": "VoicePlayer.Speak", "speech": ssml(phrase)},
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, sys.APIEndpoint+"/v1/directives", bytes.NewReader(body))
		if err != nil {
			log.Printf("progressive: %v", err)
			return
		}
		req.Header.Set("Authorization", "Bearer "+sys.APIAccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Printf("progressive: %v", err)
			return
		}
		resp.Body.Close()
		log.Printf("progressive %q status=%d", phrase, resp.StatusCode)
	}()
	return done
}

func handleQuestion(ctx context.Context, ev Request, q string, attrs Attributes) Response {
	log.Printf("question: %q", q)
	progressive := sendProgressive(ctx, ev)
	defer func() { <-progressive }() // final response must not overtake the filler
	user := ev.Session.User.UserID
	if user == "" {
		user = ev.Context.System.User.UserID
	}
	answer, err := askClaude(ctx, q, attrs.History, recall(ctx, user, ev.Session.SessionID))
	if err != nil {
		var apierr *anthropic.Error
		switch {
		case errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Timeout"):
			log.Printf("timeout: %v", err)
			return speak("Disculpe, tardé demasiado pensando. ¿Puede repetir la pregunta de forma más corta?", false, attrs)
		case errors.As(err, &apierr) && apierr.StatusCode == 429:
			return speak("Estoy recibiendo demasiadas peticiones. Inténtelo en un momento.", false, attrs)
		case errors.As(err, &apierr):
			log.Printf("Claude API error %d (request %s): %v", apierr.StatusCode, apierr.RequestID, err)
			return speak("Hubo un problema al consultar a Claude.", false, attrs)
		default:
			log.Printf("Claude error: %v", err)
			return speak("No pude conectarme con Claude.", false, attrs)
		}
	}
	remember(ctx, user, ev.Session.SessionID, q, answer)
	h := append(attrs.History, Turn{"user", q}, Turn{"assistant", answer})
	if len(h) > 2*maxTurns {
		h = h[len(h)-2*maxTurns:]
	}
	return speak(answer, false, Attributes{History: h})
}

func handler(ctx context.Context, ev Request) (Response, error) {
	appID := ev.Session.Application.ApplicationID
	if appID == "" {
		appID = ev.Context.System.Application.ApplicationID
	}
	if skillID != "" && appID != skillID {
		return Response{}, errors.New("invalid applicationId")
	}
	attrs := ev.Session.Attributes
	log.Printf("request type=%s intent=%s", ev.Request.Type, ev.Request.Intent.Name)

	switch ev.Request.Type {
	case "LaunchRequest":
		return speak("A sus órdenes. ¿En qué puedo ayudarle?", false, Attributes{}), nil
	case "SessionEndedRequest":
		log.Printf("session ended: locale=%s reason=%s error=%s %s", ev.Request.Locale, ev.Request.Reason, ev.Request.Error.Type, ev.Request.Error.Message)
		return Response{Version: "1.0"}, nil
	case "IntentRequest":
	default:
		return speak("No entendí eso.", false, attrs), nil
	}

	// Alexa strips the carrier phrase from AMAZON.SearchQuery; restore it so
	// "cuándo nació Bolívar" doesn't reach Claude as just "nació Bolívar".
	if prefix, ok := questionIntents[ev.Request.Intent.Name]; ok {
		q := ev.Request.Intent.Slots["query"].Value
		if q == "" {
			return speak("¿Qué desea preguntarme?", false, attrs), nil
		}
		return handleQuestion(ctx, ev, prefix+" "+q, attrs), nil
	}

	switch ev.Request.Intent.Name {
	case "WeatherIntent":
		return handleQuestion(ctx, ev, weatherQuestion(ev.Request.Intent.Slots["city"].Value, ev.Request.Intent.Slots["day"].Value), attrs), nil
	case "ChatIntent":
		q := ev.Request.Intent.Slots["query"].Value
		if q == "" {
			return speak("¿Qué desea preguntarme?", false, attrs), nil
		}
		return handleQuestion(ctx, ev, q, attrs), nil
	case "AskIntent":
		// Carrier said alone ("pregunta", "alfred"): wait for the question
		// instead of letting a slot-less intent like WeatherIntent win.
		return speak("Dígame, señor.", false, attrs), nil
	case "AMAZON.StopIntent", "AMAZON.CancelIntent", "AMAZON.NavigateHomeIntent":
		return speak("Hasta luego, señor.", true, Attributes{}), nil
	case "AMAZON.HelpIntent":
		return speak("Hágame cualquier pregunta, por ejemplo: pregunta cuál es la capital de Australia.", false, attrs), nil
	case "AMAZON.FallbackIntent":
		return speak("No le entendí. Empiece con: pregunta, o dime, seguido de su duda.", false, attrs), nil
	}
	return speak("No entendí eso.", false, attrs), nil
}

// warmUp builds the AWS config and clients during Lambda INIT, which overlaps
// with the cheap LaunchRequest, so the first question doesn't pay for SSM and
// config loading. Failures are only logged: getClient/getDDB retry on demand.
func warmUp() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := getClient(ctx); err != nil {
		log.Printf("warm up claude client: %v", err)
	}
	if _, err := getDDB(ctx); err != nil {
		log.Printf("warm up dynamodb: %v", err)
	}
	log.Printf("warm up done in %s", time.Since(start).Round(time.Millisecond))
}

func main() {
	warmUp()
	lambda.Start(handler)
}
