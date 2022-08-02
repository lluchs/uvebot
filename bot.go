package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// InitBot sets up and starts the discord bot.
func InitBot(token string) (*discordgo.Session, error) {
	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// In this example, we only care about receiving message events.
	dg.Identify.Intents = discordgo.IntentsGuildMessages

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
	case "!get-current-projects":
		projects, err := getCurrentProjects(s, m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		var msg strings.Builder
		for _, project := range projects {
			fmt.Fprintf(&msg, "- %s due %s\n", project.ID, project.Deadline.Format("2006-01-02"))
		}
		s.ChannelMessageSend(m.ChannelID, msg.String())

	case "!get-website-projects":
		projects, err := getWebsiteProjects()
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		var msg strings.Builder
		for _, project := range projects {
			fmt.Fprintf(&msg, "- %s due %s\n", project.ID, project.Deadline.Format("2006-01-02"))
		}
		s.ChannelMessageSend(m.ChannelID, msg.String())

	case "!check-projects":
		res, err := checkCurrentProjects(s, m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		if res == "" {
			res = "All good!"
		}
		s.ChannelMessageSend(m.ChannelID, res)

	case "!check-releases":
		res, err := checkReleases(yt)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		if res == "" {
			res = "All good!"
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
