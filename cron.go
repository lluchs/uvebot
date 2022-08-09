package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
	"google.golang.org/api/sheets/v4"
	"google.golang.org/api/youtube/v3"
)

// InitCron sets up the cronjobs.
func InitCron(dg *discordgo.Session) *cron.Cron {
	// cronjob setup
	c := cron.New()
	c.AddFunc(CheckWebsiteSchedule, func() { checkWebsiteCron(dg, yt) })
	c.AddFunc(CheckHRSchedule, func() { checkHRCron(dg, sheetsService) })
	c.Start()
	return c
}

func checkWebsiteCron(s *discordgo.Session, yt *youtube.Service) {
	projectsRes, err := checkCurrentProjects(s, UVEGuildID)
	if err != nil {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-projects error: %s", err))
		return
	}
	releasesRes, err := checkReleases(yt)
	if err != nil {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-releases error: %s", err))
		return
	}
	res := projectsRes + releasesRes
	if res != "" {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("<@&%s>\n%s", TechTeamRoleID, res))
	}
}

func checkHRCron(s *discordgo.Session, sheetsService *sheets.Service) {
	msg, err := checkHostResponses(sheetsService)
	if err != nil {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-host-responses error: %s", err))
		return
	}
	if msg != "" {
		s.ChannelMessageSend(MusicTeamChannelID, msg)
	}
}