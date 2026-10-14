package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	absentCallback = "absent"
	readyCallback  = "ready"
)

type Bot struct {
	api      *API
	cfg      *Config
	store    *Store
	username string

	// Guards store and every Telegram call that depends on it: the update
	// loop and the scheduler both mutate today's duty.
	mu sync.Mutex

	// Latest duty snapshot for the HTTP API, readable without waiting on mu.
	feed *Feed
}

func NewBot(api *API, cfg *Config, store *Store) *Bot {
	return &Bot{api: api, cfg: cfg, store: store, feed: NewFeed()}
}

func (b *Bot) Run(ctx context.Context) error {
	me, err := b.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe (check BOT_TOKEN): %w", err)
	}
	b.username = me.Username
	log.Printf("running as @%s", b.username)

	if err := b.api.SetMyCommands(ctx, []BotCommand{
		{Command: "tea", Description: "Pick someone to make tea now"},
		{Command: "members", Description: "Show who can be picked"},
		{Command: "chatid", Description: "Show this chat's id"},
	}); err != nil {
		log.Printf("setMyCommands: %v", err)
	}

	b.mu.Lock()
	b.publish()
	b.mu.Unlock()
	if b.cfg.HTTPAddr != "" {
		go b.serveHTTP(ctx)
	}

	if b.cfg.ChatID == 0 {
		log.Printf("CHAT_ID is not set: add the bot to your group, send /chatid there, then restart with CHAT_ID")
	} else {
		b.seedAdmins(ctx)
		go b.schedule(ctx)
	}

	return b.poll(ctx)
}

func (b *Bot) poll(ctx context.Context) error {
	offset := 0
	for {
		updates, err := b.api.GetUpdates(ctx, offset)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handle(ctx, u)
		}
	}
}

func (b *Bot) schedule(ctx context.Context) {
	for {
		next := nextRun(time.Now(), b.cfg)
		log.Printf("next tea pick at %s", next.Format(time.RFC1123))
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		b.mu.Lock()
		b.pick(ctx)
		b.mu.Unlock()
	}
}

// nextRun returns the next configured weekday pick time strictly after now.
func nextRun(now time.Time, cfg *Config) time.Time {
	now = now.In(cfg.Location)
	next := time.Date(now.Year(), now.Month(), now.Day(), cfg.Hour, cfg.Minute, 0, 0, cfg.Location)
	for !next.After(now) || isWeekend(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

func (b *Bot) handle(ctx context.Context, u Update) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch {
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.ChatMember != nil:
		b.handleChatMember(u.ChatMember)
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *Message) {
	cmd := b.command(m.Text)

	if cmd == "chatid" {
		b.reply(ctx, m.Chat.ID, fmt.Sprintf("Chat id: <code>%d</code>", m.Chat.ID))
		return
	}
	if m.Chat.ID != b.cfg.ChatID {
		return
	}

	changed := false
	if m.From != nil {
		changed = b.store.Upsert(*m.From) || changed
	}
	for _, nu := range m.NewChatMembers {
		changed = b.store.Upsert(nu) || changed
	}
	if m.LeftChatMember != nil {
		changed = b.store.Remove(m.LeftChatMember.ID) || changed
	}
	if changed {
		b.save()
	}

	switch cmd {
	case "tea":
		b.pick(ctx)
	case "members":
		b.reply(ctx, m.Chat.ID, b.membersText())
	}
}

// command extracts "tea" from "/tea" or "/tea@ThisBot", ignoring commands addressed to other bots.
func (b *Bot) command(text string) string {
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	word := strings.Fields(text)[0][1:]
	name, target, _ := strings.Cut(word, "@")
	if target != "" && !strings.EqualFold(target, b.username) {
		return ""
	}
	return strings.ToLower(name)
}

func (b *Bot) handleChatMember(cm *ChatMemberUpdated) {
	if cm.Chat.ID != b.cfg.ChatID {
		return
	}
	var changed bool
	switch cm.NewChatMember.Status {
	case "left", "kicked":
		changed = b.store.Remove(cm.NewChatMember.User.ID)
	default:
		changed = b.store.Upsert(cm.NewChatMember.User)
	}
	if changed {
		b.save()
	}
}

func (b *Bot) handleCallback(ctx context.Context, cq *CallbackQuery) {
	known := cq.Data == absentCallback || cq.Data == readyCallback
	if !known || cq.Message == nil || cq.Message.Chat.ID != b.cfg.ChatID {
		b.answer(ctx, cq.ID, "", false)
		return
	}

	duty := b.store.Duty
	if duty == nil || duty.MessageID != cq.Message.MessageID || duty.Date != b.today() {
		b.answer(ctx, cq.ID, "This tea round is already over 🍂", false)
		b.removeKeyboard(ctx, cq.Message.MessageID)
		return
	}
	if cq.From.ID != duty.UserID {
		b.answer(ctx, cq.ID, "🙅 Only the chosen brewer can press this!", true)
		return
	}

	member := b.store.Members[duty.UserID]
	if member == nil {
		member = &Member{ID: cq.From.ID, Username: cq.From.Username, FirstName: cq.From.FirstName, LastName: cq.From.LastName}
	}

	switch cq.Data {
	case readyCallback:
		b.removeKeyboard(ctx, duty.MessageID)
		now := time.Now()
		duty.MessageID = 0
		duty.ReadyAt = &now
		b.dutyChanged()
		b.answer(ctx, cq.ID, "You're a legend 🏆", false)
		b.reply(ctx, b.cfg.ChatID, readyMessage(mentionHTML(member)))
	case absentCallback:
		duty.Absent = append(duty.Absent, duty.UserID)
		// Plain name, no @, so the absent person isn't pinged again.
		if err := b.api.EditMessageText(ctx, b.cfg.ChatID, duty.MessageID,
			fmt.Sprintf("🏝️ <s>%s</s> is absent today, rerolling the kettle… 🎲", html.EscapeString(plainName(member)))); err != nil {
			log.Printf("edit absent message: %v", err)
		}
		duty.MessageID = 0
		b.answer(ctx, cq.ID, "Got it, picking someone else.", false)
		b.pick(ctx)
	}
}

// pick selects someone for today's tea and posts the message. Caller holds b.mu.
func (b *Bot) pick(ctx context.Context) {
	today := b.today()
	duty := b.store.Duty
	if duty == nil || duty.Date != today {
		duty = &Duty{Date: today}
		// Yesterday's button is dead now.
		if b.store.Duty != nil && b.store.Duty.MessageID != 0 {
			b.removeKeyboard(ctx, b.store.Duty.MessageID)
		}
		b.store.Duty = duty
	}
	if duty.MessageID != 0 {
		b.removeKeyboard(ctx, duty.MessageID)
		duty.MessageID = 0
	}

	candidates := eligible(b.store.Members, b.cfg, duty.Absent, duty.UserID)
	if len(candidates) == 0 {
		duty.UserID = 0
		duty.AssignmentID = 0
		duty.ReadyAt = nil
		b.dutyChanged()
		b.reply(ctx, b.cfg.ChatID, "😱 Nobody is available to make tea today. Everyone fends for themselves! 🫠")
		return
	}

	chosen := candidates[rand.IntN(len(candidates))]
	text := pickMessage(mentionHTML(chosen))
	keyboard := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
		{Text: "🏝️ Absent", CallbackData: absentCallback},
		{Text: "☕ Tea is ready", CallbackData: readyCallback},
	}}}
	msg, err := b.api.SendMessage(ctx, b.cfg.ChatID, text, keyboard)
	if err != nil {
		log.Printf("send tea message: %v", err)
		return
	}
	b.store.LastAssignment++
	duty.UserID = chosen.ID
	duty.MessageID = msg.MessageID
	duty.AssignmentID = b.store.LastAssignment
	duty.AssignedAt = time.Now()
	duty.ReadyAt = nil
	b.dutyChanged()
	log.Printf("picked %s (%d)", displayName(chosen), chosen.ID)
}

// eligible returns members that may be picked: not excluded, not absent today, and
// not the previous pick of the day unless they are the only one left.
func eligible(members map[int64]*Member, cfg *Config, absent []int64, previous int64) []*Member {
	var out []*Member
	for _, m := range members {
		if cfg.IsExcluded(m) || slices.Contains(absent, m.ID) {
			continue
		}
		out = append(out, m)
	}
	if len(out) > 1 {
		out = slices.DeleteFunc(out, func(m *Member) bool { return m.ID == previous })
	}
	// Map order is random; sort so the result is deterministic for a given input.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (b *Bot) membersText() string {
	var in, out []string
	for _, m := range b.store.Members {
		// No @ so listing doesn't ping everyone.
		name := html.EscapeString(displayName(m))
		if m.Username != "" {
			name += " (" + html.EscapeString(m.Username) + ")"
		}
		if b.cfg.IsExcluded(m) {
			out = append(out, name)
		} else {
			in = append(in, name)
		}
	}
	sort.Strings(in)
	sort.Strings(out)

	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>Tea rotation (%d)</b>\n", len(in))
	for _, n := range in {
		sb.WriteString("• " + n + "\n")
	}
	if len(out) > 0 {
		fmt.Fprintf(&sb, "\n<b>Excluded (%d)</b>\n", len(out))
		for _, n := range out {
			sb.WriteString("• " + n + "\n")
		}
	}
	sb.WriteString("\nMissing someone? They'll be added once they write anything in this chat.")
	return sb.String()
}

// seedAdmins adds group admins to the rotation; regular members are learned as they write.
func (b *Bot) seedAdmins(ctx context.Context) {
	admins, err := b.api.GetChatAdministrators(ctx, b.cfg.ChatID)
	if err != nil {
		log.Printf("getChatAdministrators: %v", err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	changed := false
	for _, a := range admins {
		changed = b.store.Upsert(a.User) || changed
	}
	if changed {
		b.save()
	}
}

func (b *Bot) today() string {
	return time.Now().In(b.cfg.Location).Format(time.DateOnly)
}

// dutyChanged persists a change to today's duty and notifies HTTP pollers. Caller holds b.mu.
func (b *Bot) dutyChanged() {
	b.store.Version++
	b.save()
	b.publish()
}

// publish pushes the current duty to the HTTP feed. Caller holds b.mu.
func (b *Bot) publish() {
	b.feed.Publish(dutyStatus(&b.store.State))
}

func (b *Bot) save() {
	if err := b.store.Save(); err != nil {
		log.Printf("save state: %v", err)
	}
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if _, err := b.api.SendMessage(ctx, chatID, text, nil); err != nil {
		log.Printf("send message: %v", err)
	}
}

func (b *Bot) answer(ctx context.Context, id, text string, alert bool) {
	if err := b.api.AnswerCallbackQuery(ctx, id, text, alert); err != nil {
		log.Printf("answerCallbackQuery: %v", err)
	}
}

func (b *Bot) removeKeyboard(ctx context.Context, messageID int) {
	if err := b.api.RemoveKeyboard(ctx, b.cfg.ChatID, messageID); err != nil {
		log.Printf("remove keyboard: %v", err)
	}
}

func displayName(m *Member) string {
	return strings.TrimSpace(m.FirstName + " " + m.LastName)
}

// mentionHTML pings the member: @username when they have one, a tg:// link otherwise.
func mentionHTML(m *Member) string {
	if m.Username != "" {
		return "@" + html.EscapeString(m.Username)
	}
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, m.ID, html.EscapeString(displayName(m)))
}

// plainName names a member without pinging them.
func plainName(m *Member) string {
	if m.Username != "" {
		return m.Username
	}
	return displayName(m)
}
