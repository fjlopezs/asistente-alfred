package main

import (
	"encoding/json"
	"os"
	"testing"
)

// The Go map and the interaction model must list the same question intents.
func TestQuestionIntentsMatchModel(t *testing.T) {
	for _, f := range []string{"../interaction-model.es-MX.json", "../interaction-model.es-US.json"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			InteractionModel struct {
				LanguageModel struct {
					Intents []struct {
						Name    string   `json:"name"`
						Samples []string `json:"samples"`
					} `json:"intents"`
				} `json:"languageModel"`
			} `json:"interactionModel"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, in := range m.InteractionModel.LanguageModel.Intents {
			prefix, ok := questionIntents[in.Name]
			if !ok {
				continue
			}
			seen++
			if len(in.Samples) != 1 || in.Samples[0] != prefix+" {query}" {
				t.Errorf("%s: %s samples %v, want %q", f, in.Name, in.Samples, prefix+" {query}")
			}
		}
		if seen != len(questionIntents) {
			t.Errorf("%s: %d question intents in model, %d in code", f, seen, len(questionIntents))
		}
	}
}
