package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"time"
)

// JokeAPI (https://jokeapi.dev): free, no key. safe-mode keeps it work-friendly.
var jokeURL = "https://v2.jokeapi.dev/joke/Any?safe-mode"

var jokeClient = &http.Client{Timeout: 10 * time.Second}

type joke struct {
	Error    bool   `json:"error"`
	Type     string `json:"type"` // "single" or "twopart"
	Joke     string `json:"joke"`
	Setup    string `json:"setup"`
	Delivery string `json:"delivery"`
}

func (b *Bot) tellJoke(ctx context.Context, chatID int64) {
	text, err := fetchJoke(ctx)
	if err != nil {
		log.Printf("joke: %v", err)
		text = "🤷 The joke machine is out of tea. Try again later!"
	}
	b.reply(ctx, chatID, text)
}

// fetchJoke returns a random joke as HTML; two-part jokes hide the punchline in a spoiler.
func fetchJoke(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jokeURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := jokeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %s", resp.Status)
	}

	var j joke
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	switch {
	case j.Error:
		return "", errors.New("api returned an error")
	case j.Type == "twopart" && j.Setup != "" && j.Delivery != "":
		return "😄 " + html.EscapeString(j.Setup) + "\n\n<tg-spoiler>" + html.EscapeString(j.Delivery) + "</tg-spoiler>", nil
	case j.Joke != "":
		return "😄 " + html.EscapeString(j.Joke), nil
	}
	return "", errors.New("empty joke")
}
