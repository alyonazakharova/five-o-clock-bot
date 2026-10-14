package main

import (
	"math/rand/v2"
	"strings"
)

// {who} is replaced with the HTML mention of the person. Templates must be HTML-safe.

var pickMessages = []string{
	"🫖 Attention! The Tea Council has spoken: {who}, you are today's Grand Brewmaster. Please make tea ☕✨",
	"🚨 Tea emergency! 🚨 {who}, the kettle has chosen you. Please make tea before we all dry out 🏜️☕",
	"🎲 The dice were rolled, the stars aligned, the kettle whistled… {who}, it's you! Please make tea 🫖🌟",
	"👑 By royal decree of the Five O'Clock Committee, {who} shall make the tea. Long live the brewer! ☕🍪",
	"🐉 A wild tea duty appeared! {who} was chosen. Please make tea, brave adventurer 🗡️🫖",
	"📢 {who}, your mission, should you choose to accept it (you don't have a choice): please make tea 🕶️☕",
	"🍵 It's five o'clock somewhere, and here too! {who}, please make tea for the thirsty crowd 🥺🫖",
	"🧙 The ancient tea leaves foretold this moment: {who} will make tea today. Please don't anger the leaves 🍃☕",
}

var readyMessages = []string{
	"☕✨ Tea is ready! Huge thanks to {who}, hero of the kettle. Everyone, drop what you're doing and come get a cup 🫖🍪",
	"🔔 Ding ding ding! {who} has brewed the elixir of productivity. Tea time, everybody! 🍵🎉",
	"🫖 Fresh tea alert! {who} did the thing. Grab a mug before it gets cold and sad 🥶☕",
	"🏆 {who} has completed today's quest: TEA. Loot is in the kitchen — come and claim your cup! 🍪☕",
	"🎺 Hear ye, hear ye! Tea has been served by the magnificent {who}. To the kitchen! 🏃‍♀️🏃‍♂️🍵",
	"🌿 The leaves have steeped, the cups await. Thank you {who}! Everyone, time for a tea break 🧁☕",
}

func pickMessage(who string) string {
	return fill(pickMessages, who)
}

func readyMessage(who string) string {
	return fill(readyMessages, who)
}

func fill(templates []string, who string) string {
	return strings.ReplaceAll(templates[rand.IntN(len(templates))], "{who}", who)
}
