// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestSendProgressive(t *testing.T) {
	var got map[string]map[string]string
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	var ev Request
	ev.Context.System.APIEndpoint = srv.URL
	ev.Context.System.APIAccessToken = "tok"
	ev.Request.RequestID = "req-1"
	<-sendProgressive(context.Background(), ev)

	if path != "/v1/directives" || auth != "Bearer tok" ||
		got["header"]["requestId"] != "req-1" || got["directive"]["type"] != "VoicePlayer.Speak" {
		t.Fatalf("bad request: path=%s auth=%s body=%v", path, auth, got)
	}
	s := got["directive"]["speech"]
	if !slices.ContainsFunc(thinkingPhrases, func(p string) bool { return s == ssml(p) }) {
		t.Fatalf("unexpected speech %q", s)
	}
}

func TestSendProgressiveSkipsWithoutToken(t *testing.T) {
	<-sendProgressive(context.Background(), Request{}) // must not block or panic
}
