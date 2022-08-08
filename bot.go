package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/bwmarrin/discordgo"
)

// InitBot sets up and starts the discord bot.
func InitBot(token string, listen bool) (*discordgo.Session, error) {
	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	if listen {
		// Register the messageCreate func as a callback for MessageCreate events.
		dg.AddHandler(messageCreate)

		// In this example, we only care about receiving message events.
		dg.Identify.Intents = discordgo.IntentsGuildMessages
	}

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		return nil, err
	}
	return dg, nil
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {

	// Ignore all messages created by the bot itself
	// This isn't required in this specific example but it's a good practice.
	if m.Author.ID == s.State.User.ID {
		return
	}

	switch m.Content {
	case "!get-current-projects",
		"!get-website-projects",
		"!check-projects",
		"!check-releases",
		"!check-host-responses":
		res, err := handleCommand(m.Content, s)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		s.ChannelMessageSend(m.ChannelID, res)

	case "HONK":
		if m.ChannelID == HonkChannelID && rand.Int31n(100) < HonkChance {
			go func() {
				delay := rand.Int31n(HonkDelay * 60)
				time.Sleep(time.Duration(delay) * time.Second)
				s.ChannelMessageSend(HonkChannelID, "HONK")
			}()
		}
	}
}
