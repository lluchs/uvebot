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
	responses, err := checkHostResponses(sheetsService)
	if err != nil {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-host-responses error: %s", err))
		return
	}
	for _, response := range responses {
		channel, err := createProposedProjectChannel(s, &response)
		if err != nil {
			s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-host-responses error: %s", err))
		}
		s.ChannelMessageSend(MusicTeamChannelID, response.Message+fmt.Sprintf(" <#%s>", channel.ID))
	}
}

func createProposedProjectChannel(s *discordgo.Session, response *hostResponse) (*discordgo.Channel, error) {
	channel, err := s.GuildChannelCreateComplex(UVEGuildID, discordgo.GuildChannelCreateData{
		Name:     response.Slug,
		Topic:    response.Name,
		ParentID: HostResponsesCategID,
	})

	if err != nil {
		return nil, fmt.Errorf("could not create channel #%s: %w", response.Slug, err)
	}
	var embed discordgo.MessageEmbed
	for _, val := range response.Response {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   val[0],
			Value:  val[1],
			Inline: false,
		})
	}
	_, err = s.ChannelMessageSendEmbed(channel.ID, &embed)
	if err != nil {
		return nil, fmt.Errorf("could not send message to #%s: %w", response.Slug, err)
	}
	return channel, nil
}
