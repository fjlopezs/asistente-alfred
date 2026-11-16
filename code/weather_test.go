// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetWeather(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/geo") {
			if r.URL.Query().Get("name") != "Valencia" {
				t.Errorf("geocode name=%q", r.URL.Query().Get("name"))
			}
			w.Write([]byte(`{"results":[
				{"name":"Valencia","admin1":"Comunidad Valenciana","country":"España","latitude":39.4,"longitude":-0.3},
				{"name":"Valencia","admin1":"Carabobo","country":"Venezuela","latitude":10.1,"longitude":-68}]}`))
			return
		}
		if r.URL.Query().Get("latitude") != "10.1" {
			t.Errorf("wrong place picked: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"current":{"time":"2026-11-14T20:00","temperature_2m":28.5,"weather_code":61},
			"daily":{"time":["2026-11-14"],"weather_code":[95],"temperature_2m_max":[31],
				"temperature_2m_min":[22],"precipitation_probability_max":[80]}}`))
	}))
	defer srv.Close()
	geocodeURL, forecastURL = srv.URL+"/geo", srv.URL+"/fc"

	out, err := getWeather(context.Background(), "Valencia, Venezuela")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Lugar      string
		Ahora      struct{ Cielo string }
		Pronostico []struct{ Cielo string }
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Lugar, "Venezuela") || got.Ahora.Cielo != "lluvia" || got.Pronostico[0].Cielo != "tormenta" {
		t.Fatalf("unexpected %s", out)
	}
}

func TestGetWeatherNoCity(t *testing.T) {
	defaultCity = ""
	if _, err := getWeather(context.Background(), ""); err == nil {
		t.Fatal("expected error without city")
	}
}

func TestWeatherQuestion(t *testing.T) {
	if got := weatherQuestion("Valdivia", "mañana"); got != "¿Cómo está el clima en Valdivia mañana? ¿Va a llover?" {
		t.Fatal(got)
	}
	if got := weatherQuestion("", ""); got != "¿Cómo está el clima? ¿Va a llover?" {
		t.Fatal(got)
	}
}

func TestWMO(t *testing.T) {
	cases := map[int]string{
		0: "despejado", 2: "parcialmente nublado", 3: "nublado", 45: "niebla", 53: "llovizna",
		63: "lluvia", 75: "nieve", 81: "chubascos", 86: "chubascos de nieve", 95: "tormenta",
	}
	for code, want := range cases {
		if got := wmo(code); got != want {
			t.Errorf("wmo(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestGetWeatherErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") == "Atlantis" {
			w.Write([]byte(`{"results":[]}`))
			return
		}
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	oldGeo, oldFc := geocodeURL, forecastURL
	geocodeURL, forecastURL = srv.URL+"/geo", srv.URL+"/fc"
	t.Cleanup(func() { geocodeURL, forecastURL = oldGeo, oldFc })

	_, err := getWeather(t.Context(), "Atlantis")
	if err == nil || !strings.Contains(err.Error(), "no encontré la ciudad") {
		t.Errorf("unknown city: %v", err)
	}
	if _, err := getWeather(t.Context(), "Temuco"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("geocoding outage: %v", err)
	}
}
