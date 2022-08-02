package main

const (
	WebsiteURL            = "https://www.untitledvirtualensemble.org" // URL of the UVE website
	WebsiteReleasesURL    = WebsiteURL + "/released-performances"     // URL of the releases page of the UVE website
	UVEGuildID            = "851213338481655878"                      // ID of the UVE guild
	TechTeamRoleID        = "851304372976746497"                      // ID of the @Teach Team role
	TechTeamChannelID     = "909798620281311312"                      // ID of the #tech-team channel
	HonkChannelID         = "870342886745600021"                      // ID of the #geese-go-honk channel
	StaffBotSpamChannelID = "924839541959983124"                      // ID of the #staff-bot-spam channel
	UVEPlaylistID         = "PLhCTe78BMQ8VoO7aCZYrZpdBKqCEqvMMg"      // youtube playlist with all videos
	CheckWebsiteSchedule  = "0 12 * * *"                              // cron configuration for the website check
	HonkChance            = 33                                        // chance to reply to a HONK in %
	HonkDelay             = 30                                        // maximum delay until HONK reply in minutes
)
