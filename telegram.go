package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Minimal Telegram Bot API client — only what the bot needs.

type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

type Message struct {
	MessageID      int    `json:"message_id"`
	From           *User  `json:"from,omitempty"`
	Chat           Chat   `json:"chat"`
	Text           string `json:"text,omitempty"`
	NewChatMembers []User `json:"new_chat_members,omitempty"`
	LeftChatMember *User  `json:"left_chat_member,omitempty"`

	ReplyToMessage *Message        `json:"reply_to_message,omitempty"`
	Entities       []MessageEntity `json:"entities,omitempty"`
}

type MessageEntity struct {
	Type string `json:"type"`
	User *User  `json:"user,omitempty"` // set for "text_mention" (people without a username)
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

type ChatMemberUpdated struct {
	Chat          Chat       `json:"chat"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

type Update struct {
	UpdateID      int                `json:"update_id"`
	Message       *Message           `json:"message,omitempty"`
	CallbackQuery *CallbackQuery     `json:"callback_query,omitempty"`
	ChatMember    *ChatMemberUpdated `json:"chat_member,omitempty"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

type API struct {
	base   string
	client *http.Client
}

const pollTimeoutSec = 50

func NewAPI(token string) *API {
	return &API{
		base:   "https://api.telegram.org/bot" + token + "/",
		client: &http.Client{Timeout: (pollTimeoutSec + 20) * time.Second},
	}
}

func (a *API) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		// Don't leak the token-bearing URL into logs.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: request failed", method)
	}
	defer resp.Body.Close()

	var r apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if !r.OK {
		return fmt.Errorf("%s: %d %s", method, r.ErrorCode, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

func (a *API) GetMe(ctx context.Context) (User, error) {
	var u User
	err := a.call(ctx, "getMe", map[string]any{}, &u)
	return u, err
}

func (a *API) GetUpdates(ctx context.Context, offset int) ([]Update, error) {
	var updates []Update
	err := a.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         pollTimeoutSec,
		"allowed_updates": []string{"message", "callback_query", "chat_member"},
	}, &updates)
	return updates, err
}

func (a *API) SendMessage(ctx context.Context, chatID int64, html string, markup *InlineKeyboardMarkup) (Message, error) {
	params := map[string]any{
		"chat_id":    chatID,
		"text":       html,
		"parse_mode": "HTML",
	}
	if markup != nil {
		params["reply_markup"] = markup
	}
	var m Message
	err := a.call(ctx, "sendMessage", params, &m)
	return m, err
}

// EditMessageText replaces the text and drops the inline keyboard.
func (a *API) EditMessageText(ctx context.Context, chatID int64, messageID int, html string) error {
	return a.call(ctx, "editMessageText", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       html,
		"parse_mode": "HTML",
	}, nil)
}

// RemoveKeyboard drops the inline keyboard from a message, keeping its text.
func (a *API) RemoveKeyboard(ctx context.Context, chatID int64, messageID int) error {
	return a.call(ctx, "editMessageReplyMarkup", map[string]any{
		"chat_id":      chatID,
		"message_id":   messageID,
		"reply_markup": InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}},
	}, nil)
}

func (a *API) AnswerCallbackQuery(ctx context.Context, id, text string, alert bool) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": id,
		"text":              text,
		"show_alert":        alert,
	}, nil)
}

func (a *API) GetChatAdministrators(ctx context.Context, chatID int64) ([]ChatMember, error) {
	var members []ChatMember
	err := a.call(ctx, "getChatAdministrators", map[string]any{"chat_id": chatID}, &members)
	return members, err
}

func (a *API) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return a.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}
