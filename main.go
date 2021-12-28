package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
)

const (
	WebsiteURL            = "https://www.untitledvirtualensemble.org" // URL of the UVE website
	UVEGuildID            = "851213338481655878"                      // ID of the UVE guild
	TechTeamRoleID        = "851304372976746497"                      // ID of the @Teach Team role
	TechTeamChannelID     = "909798620281311312"                      // ID of the #tech-team channel
	CheckProjectsSchedule = "0 12 * * *"                              // cron configuration for the projects check
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")

	// Create a new Discord session using the provided bot token.
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(messageCreate)

	// In this example, we only care about receiving message events.
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	// Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	// cronjob setup
	c := cron.New()
	c.AddFunc(CheckProjectsSchedule, func() { checkProjectsCron(dg) })
	c.Start()

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	c.Stop()
	// Cleanly close down the Discord session.
	dg.Close()
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {

	// Ignore all messages created by the bot itself
	// This isn't required in this specific example but it's a good practice.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!get-current-projects" {
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
	}

	if m.Content == "!get-website-projects" {
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
	}

	if m.Content == "!check-projects" {
		res, err := checkCurrentProjects(s, m.GuildID)
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("error: %s", err))
			return
		}
		if res == "" {
			res = "All good!"
		}
		s.ChannelMessageSend(m.ChannelID, res)
	}
}

func checkProjectsCron(s *discordgo.Session) {
	res, err := checkCurrentProjects(s, UVEGuildID)
	if err != nil {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("!check-projects error: %s", err))
		return
	}
	if res != "" {
		s.ChannelMessageSend(TechTeamChannelID, fmt.Sprintf("<@&%s>\n%s", TechTeamRoleID, res))
	}
}

// Project contains information about a UVE project.
type Project struct {
	ID       string // channel name / URL slug
	Name     string
	Channel  *discordgo.Channel
	Deadline time.Time
}

// relativeYear sets the year so that the timestamp is closest to the reference timestamp.
func relativeYear(time, ref time.Time) time.Time {
	t := time.AddDate(ref.Year(), 0, 0)
	if t.Month() < ref.Month() && ref.Sub(t).Hours() > 365/2*24 {
		t = t.AddDate(1, 0, 0)
	}
	return t
}

func parseProject(msg *discordgo.Message, channels []*discordgo.Channel) (*Project, error) {
	lines := strings.Split(msg.Content, "\n")
	var p Project
	p.Name = lines[0]
	for _, line := range lines {
		// Deadline: December 29 (Extension)
		if strings.HasPrefix(line, "Deadline: ") {
			parts := strings.Split(line, " ")
			t, err := time.Parse("January 2", fmt.Sprintf("%s %s", parts[1], strings.ReplaceAll(parts[2], "th", "")))
			if err != nil {
				return nil, fmt.Errorf("could not parse time for %s: %w", p.Name, err)
			}
			// Set deadline year so that it is in the future.
			ctime, err := discordgo.SnowflakeTimestamp(msg.ID)
			if err != nil {
				return nil, fmt.Errorf("could not get message snowflake timestamp for %s: %w", msg.ID, err)
			}
			p.Deadline = relativeYear(t, ctime)
		} else if strings.HasPrefix(line, "<#") {
			cid := strings.Trim(line, "<#> ")
			for _, c := range channels {
				if c.ID == cid {
					p.Channel = c
					p.ID = c.Name
					break
				}
			}
		}
	}
	return &p, nil
}

// checkCurrentProjects compares the projects in #current-projects and the website.
func checkCurrentProjects(s *discordgo.Session, guildID string) (string, error) {
	projects, err := getCurrentProjects(s, guildID)
	if err != nil {
		return "", err
	}
	website, err := getWebsiteProjects()
	if err != nil {
		return "", err
	}

	projectsMap := make(map[string]*Project)
	websiteMap := make(map[string]*Project)
	buildMap := func(projects []*Project, m map[string]*Project) {
		for _, p := range projects {
			m[p.ID] = p
		}
	}
	buildMap(projects, projectsMap)
	buildMap(website, websiteMap)

	var msg strings.Builder
	for id, website := range websiteMap {
		project, ok := projectsMap[id]
		if ok {
			if website.Deadline != project.Deadline {
				fmt.Fprintf(&msg, "- %s: wrong deadline (website: %s, #current-projects: %s)\n", id, website.Deadline.Format("2006-01-02"), project.Deadline.Format("2006-01-02"))
			}
			if time.Now().AddDate(0, 0, -2).After(project.Deadline) {
				fmt.Fprintf(&msg, "- %s: deadline %s has passed\n", id, project.Deadline.Format("2006-01-02"))
			}
		} else {
			fmt.Fprintf(&msg, "- %s: on website but not in #current-projects\n", id)
		}
	}
	for id, project := range projectsMap {
		if time.Now().AddDate(0, 0, -2).After(project.Deadline) {
			// deadline has passed, skip
			continue
		}
		if _, ok := websiteMap[id]; !ok {
			fmt.Fprintf(&msg, "- %s: missing on website\n", id)
		}
	}
	return msg.String(), nil
}

// getCurrentProjects retrieves current projects from the Discord channel #current-projects.
func getCurrentProjects(s *discordgo.Session, guildID string) ([]*Project, error) {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, err
	}
	var projectsChannel *discordgo.Channel
	for _, c := range channels {
		if c.Name == "current-projects" {
			projectsChannel = c
			break
		}
	}
	if projectsChannel == nil {
		return nil, fmt.Errorf("could not find #current-projects")
	}
	messages, err := s.ChannelMessages(projectsChannel.ID, 20, "", "", "")
	var projects []*Project
	for _, msg := range messages {
		p, err := parseProject(msg, channels)
		if err != nil {
			fmt.Printf("could not parse project: %s\n", err)
			continue
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// getWebsiteProjects retrieves current projects from the UVE website.
func getWebsiteProjects() ([]*Project, error) {
	res, err := http.Get(WebsiteURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d (getting %s)", res.StatusCode, WebsiteURL)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var projects []*Project
	var innerErr error

	doc.Find(`a[href^="/projects/"]`).Each(func(i int, s *goquery.Selection) {
		var p Project
		title := s.Text()
		parts := strings.SplitN(title, " - ", 2)
		due, err := time.Parse("Due Jan. 2", parts[0])
		if err != nil {
			innerErr = err
			return
		}
		p.Name = parts[1]
		p.ID = s.AttrOr("href", "/projects/")[10:]
		// For the year, assume that the website is updated at least once per month.
		p.Deadline = relativeYear(due, time.Now().AddDate(0, -1, 0))
		projects = append(projects, &p)
	})
	if innerErr != nil {
		return nil, innerErr
	}
	return projects, nil
}
