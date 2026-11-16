// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Weather tool backed by Open-Meteo (free, no API key).

var (
	defaultCity = os.Getenv("DEFAULT_CITY")
	geocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	forecastURL = "https://api.open-meteo.com/v1/forecast"
)

var weatherTool = anthropic.ToolParam{
	Name: "get_weather",
	Description: anthropic.String("Clima actual y pronóstico de 3 días (hoy, mañana, pasado mañana) para una ciudad. " +
		"Úsala para cualquier pregunta sobre clima, temperatura, lluvia o viento. " +
		"Si el usuario no menciona un lugar, omite city para usar su ciudad de residencia."),
	InputSchema: anthropic.ToolInputSchemaParam{
		Properties: map[string]any{
			"city": map[string]any{"type": "string", "description": "Ciudad, opcionalmente con país, p. ej. 'Lima, Perú'"},
		},
	},
}

type place struct {
	Name      string  `json:"name"`
	Admin1    string  `json:"admin1"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// geocode resolves "City" or "City, Region/Country". Open-Meteo only matches
// plain names, so the qualifier is used to pick among candidates.
func geocode(ctx context.Context, city string) (place, error) {
	name, qual, _ := strings.Cut(city, ",")
	name, qual = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(qual))
	var r struct{ Results []place }
	q := url.Values{"name": {name}, "count": {"10"}, "language": {"es"}}
	if err := getJSON(ctx, geocodeURL+"?"+q.Encode(), &r); err != nil {
		return place{}, err
	}
	if len(r.Results) == 0 {
		return place{}, fmt.Errorf("no encontré la ciudad %q", city)
	}
	for _, p := range r.Results {
		country, region := strings.ToLower(p.Country), strings.ToLower(p.Admin1)
		if qual != "" && (strings.Contains(country, qual) || strings.Contains(region, qual)) {
			return p, nil
		}
	}
	return r.Results[0], nil // results are sorted by population
}

// getWeather returns a compact JSON summary for Claude to narrate.
func getWeather(ctx context.Context, city string) (string, error) {
	if city == "" {
		city = defaultCity
	}
	if city == "" {
		return "", errors.New("no conozco la ciudad del usuario; pregúntale de qué ciudad quiere el clima")
	}
	p, err := geocode(ctx, city)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"latitude":  {fmt.Sprint(p.Latitude)},
		"longitude": {fmt.Sprint(p.Longitude)},
		"current": {"temperature_2m,apparent_temperature,relative_humidity_2m," +
			"precipitation,weather_code,wind_speed_10m"},
		"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max"},
		"timezone":      {"auto"},
		"forecast_days": {"3"},
	}
	var f struct {
		Current struct {
			Time        string  `json:"time"`
			Temp        float64 `json:"temperature_2m"`
			Feels       float64 `json:"apparent_temperature"`
			Humidity    float64 `json:"relative_humidity_2m"`
			Precip      float64 `json:"precipitation"`
			WeatherCode int     `json:"weather_code"`
			Wind        float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			Time       []string  `json:"time"`
			Code       []int     `json:"weather_code"`
			Max        []float64 `json:"temperature_2m_max"`
			Min        []float64 `json:"temperature_2m_min"`
			RainChance []float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := getJSON(ctx, forecastURL+"?"+q.Encode(), &f); err != nil {
		return "", err
	}
	type day struct {
		Date       string  `json:"fecha"`
		Sky        string  `json:"cielo"`
		Max        float64 `json:"max_c"`
		Min        float64 `json:"min_c"`
		RainChance float64 `json:"prob_lluvia_pct"`
	}
	days := make([]day, 0, len(f.Daily.Time))
	for i, d := range f.Daily.Time {
		if i < len(f.Daily.Code) && i < len(f.Daily.Max) && i < len(f.Daily.Min) && i < len(f.Daily.RainChance) {
			days = append(days, day{d, wmo(f.Daily.Code[i]), f.Daily.Max[i], f.Daily.Min[i], f.Daily.RainChance[i]})
		}
	}
	out, err := json.Marshal(map[string]any{
		"lugar": fmt.Sprintf("%s, %s, %s", p.Name, p.Admin1, p.Country),
		"ahora": map[string]any{
			"hora_local":  f.Current.Time,
			"cielo":       wmo(f.Current.WeatherCode),
			"temp_c":      f.Current.Temp,
			"sensacion_c": f.Current.Feels,
			"humedad_pct": f.Current.Humidity,
			"lluvia_mm":   f.Current.Precip,
			"viento_km_h": f.Current.Wind,
		},
		"pronostico": days,
	})
	return string(out), err
}

// weatherQuestion rebuilds a question from WeatherIntent slots (Alexa doesn't
// send the raw utterance).
func weatherQuestion(city, day string) string {
	q := "¿Cómo está el clima"
	if city != "" {
		q += " en " + city
	}
	if day != "" {
		q += " " + day
	}
	return q + "? ¿Va a llover?"
}

// wmo translates WMO weather codes used by Open-Meteo.
func wmo(c int) string {
	switch {
	case c == 0:
		return "despejado"
	case c <= 2:
		return "parcialmente nublado"
	case c == 3:
		return "nublado"
	case c <= 48:
		return "niebla"
	case c <= 57:
		return "llovizna"
	case c <= 67:
		return "lluvia"
	case c <= 77:
		return "nieve"
	case c <= 82:
		return "chubascos"
	case c <= 86:
		return "chubascos de nieve"
	default:
		return "tormenta"
	}
}
