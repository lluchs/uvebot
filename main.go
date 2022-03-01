package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

const (
	WebsiteURL           = "https://www.untitledvirtualensemble.org" // URL of the UVE website
	WebsiteReleasesURL   = WebsiteURL + "/released-performances"     // URL of the releases page of the UVE website
	UVEGuildID           = "851213338481655878"                      // ID of the UVE guild
	TechTeamRoleID       = "851304372976746497"                      // ID of the @Teach Team role
	TechTeamChannelID    = "909798620281311312"                      // ID of the #tech-team channel
	HonkChannelID        = "870342886745600021"                      // ID of the #geese-go-honk channel
	UVEPlaylistID        = "PLhCTe78BMQ8VoO7aCZYrZpdBKqCEqvMMg"      // youtube playlist with all videos
	CheckWebsiteSchedule = "0 12 * * *"                              // cron configuration for the website check
	HonkChance           = 33                                        // chance to reply to a HONK in %
	HonkDelay            = 30                                        // maximum delay until HONK reply in minutes
)

var yt *youtube.Service

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	youtubeKey := os.Getenv("YOUTUBE_API_KEY")

	var err error
	yt, err = youtube.NewService(context.TODO(), option.WithAPIKey(youtubeKey))
	if err != nil {
		fmt.Println("creating YouTube client failed:", err)
		return
	}

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
	c.AddFunc(CheckWebsiteSchedule, func() { checkWebsiteCron(dg, yt) })
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

// Project contains information about a UVE project.
type Project struct {
	ID       string // channel name / URL slug
	Name     string
	Channel  *discordgo.Channel
	Deadline time.Time
	URLs     []string // URLs in the body of the project page
}

// ProjectsByDeadline implements sort.Interface for []*Person based on the Deadline field.
type ProjectsByDeadline []*Project

func (a ProjectsByDeadline) Len() int           { return len(a) }
func (a ProjectsByDeadline) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ProjectsByDeadline) Less(i, j int) bool { return a[i].Deadline.Before(a[j].Deadline) }

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
	err = fetchWebsiteProjectLinks(website)
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
			if len(website.URLs) > 0 {
				err = fetchDiscordProjectLinks(s, project)
				sort.Slice(project.URLs, func(i, j int) bool {
					return project.URLs[i] < project.URLs[j]
				})
				if err != nil {
					return "", fmt.Errorf("error fetching links for %s: %w", project.ID, err)
				}
				for _, u := range website.URLs {
					// Does the URL also appear in Discord?
					idx := sort.SearchStrings(project.URLs, u)
					if idx == len(project.URLs) || project.URLs[idx] != u {
						// However, Discord links are always okay (non-PD projects)
						if !strings.HasPrefix(u, "https://discord.gg/") {
							fmt.Fprintf(&msg, "- %s: URL does not appear in channel pins %s\n", id, u)
						}
					}
				}
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
		// Chamber projects use threads which we can't retrieve for now.
		if p.ID == "" {
			continue
		}
		projects = append(projects, p)
	}
	sort.Sort(ProjectsByDeadline(projects))
	return projects, nil
}

var urlRegex *regexp.Regexp = regexp.MustCompile(`https?://[^\s]+`)

// fetchDiscordProjectLinks populates the project's URLs field from pinned messages in Discord.
func fetchDiscordProjectLinks(s *discordgo.Session, p *Project) error {
	if p.Channel == nil {
		return nil
	}
	pinned, err := s.ChannelMessagesPinned(p.Channel.ID)
	if err != nil {
		return err
	}
	for _, msg := range pinned {
		for _, match := range urlRegex.FindAllString(msg.Content, -1) {
			// remove trailing dots
			if match[len(match)-1] == '.' {
				match = match[:len(match)-1]
			}
			p.URLs = append(p.URLs, match)
		}
	}
	return nil
}

func httpGetDoc(url string) (*goquery.Document, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d (getting %s)", res.StatusCode, url)
	}
	return goquery.NewDocumentFromReader(res.Body)
}

// getWebsiteProjects retrieves current projects from the UVE website.
func getWebsiteProjects() ([]*Project, error) {
	doc, err := httpGetDoc(WebsiteURL)
	if err != nil {
		return nil, err
	}

	var projects []*Project
	var innerErr error

	doc.Find(`a[href^="/projects/"]`).Each(func(i int, s *goquery.Selection) {
		var p Project
		title := s.Text()
		parts := strings.SplitN(title, " - ", 2)
		if len(parts) != 2 {
			// skip if incorrectly formatted
			return
		}
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
	sort.Sort(ProjectsByDeadline(projects))
	return projects, nil
}

// fetchWebsiteProjectLinks populates the project's URLs field.
func fetchWebsiteProjectLinks(projects []*Project) error {
	for _, p := range projects {
		doc, err := httpGetDoc(WebsiteURL + "/projects/" + p.ID)
		if err != nil {
			return err
		}
		// Find all links in the main text
		doc.Find(`div[role=main] section:nth-child(2) a`).Each(func(i int, s *goquery.Selection) {
			if href, ok := s.Attr("href"); ok {
				if strings.HasPrefix(href, "https://www.google.com/url?q=") {
					gurl, err := url.Parse(href)
					if err != nil {
						fmt.Println("error while decoding google url: ", err)
					}
					href = gurl.Query().Get("q")
				}
				p.URLs = append(p.URLs, href)
			}
		})
	}
	return nil
}

// getYoutubeVideos retrieves UVE's youtube playlist.
func getYoutubeVideos(yt *youtube.Service) ([]youtube.PlaylistItem, error) {
	var videos []youtube.PlaylistItem
	call := yt.PlaylistItems.List([]string{"snippet", "contentDetails"}).PlaylistId(UVEPlaylistID).MaxResults(50)
	err := call.Pages(context.TODO(), func(res *youtube.PlaylistItemListResponse) error {
		for _, item := range res.Items {
			videos = append(videos, *item)
		}
		return nil
	})
	return videos, err
}

var youtubeIDRegex = regexp.MustCompile(`(?i)(?:youtube\.com\/(?:[^\/]+\/.+\/|(?:v|e(?:mbed)?)\/|.*[?&]v=)|youtu\.be\/)([^"&?\/\s]{11})`)

// getWebsiteProjects retrieves current projects from the UVE website.
func getWebsiteReleases() ([]string, error) {
	res, err := http.Get(WebsiteReleasesURL)
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

	var videoIDs []string
	var innerErr error

	doc.Find(`a`).Each(func(i int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		m := youtubeIDRegex.FindStringSubmatch(href)
		if m == nil {
			return
		}
		videoIDs = append(videoIDs, m[1])
	})
	if innerErr != nil {
		return nil, innerErr
	}
	return videoIDs, nil
}

// checkReleases compares the website release page with the YouTube playlist.
func checkReleases(yt *youtube.Service) (string, error) {
	var msg strings.Builder

	websiteIDs, err := getWebsiteReleases()
	if err != nil {
		return "", err
	}
	videos, err := getYoutubeVideos(yt)
	if err != nil {
		return "", err
	}

	videoMap := make(map[string]youtube.PlaylistItem)
	for _, v := range videos {
		videoMap[v.ContentDetails.VideoId] = v
	}
	websiteMap := make(map[string]bool)
	for _, v := range websiteIDs {
		websiteMap[v] = true
	}

	for _, v := range videos {
		if v.Snippet.Title == "Private video" {
			continue
		}
		if _, ok := websiteMap[v.ContentDetails.VideoId]; !ok {
			fmt.Fprintf(&msg, "- %s: missing on website (https://youtu.be/%s)\n", v.Snippet.Title, v.ContentDetails.VideoId)
		}
	}

	for _, v := range websiteIDs {
		if _, ok := videoMap[v]; !ok {
			fmt.Fprintf(&msg, "- https://youtu.be/%s missing in playlist\n", v)
		}
	}

	return msg.String(), nil
}
