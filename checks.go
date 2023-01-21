package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bwmarrin/discordgo"
	"google.golang.org/api/sheets/v4"
	"google.golang.org/api/youtube/v3"
)

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

var dateSuffixRegex = regexp.MustCompile(`st|nd|rd|th`)

func parseProject(msg *discordgo.Message, channels []*discordgo.Channel) (*Project, error) {
	lines := strings.Split(msg.Content, "\n")
	var p Project
	p.Name = lines[0]
	for _, line := range lines {
		// Deadline: December 29 (Extension)
		if strings.HasPrefix(line, "Deadline: ") {
			parts := strings.Split(line, " ")
			if parts[1] == "--" {
				// Return an empty project which is skipped later.
				p.ID = ""
				return &p, nil
			}
			if len(parts) < 3 {
				return nil, fmt.Errorf("could not parse time for %s: not enough words", p.Name)
			}
			t, err := time.Parse("January 2", fmt.Sprintf("%s %s", parts[1], dateSuffixRegex.ReplaceAllString(parts[2], "")))
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
			errmsg := fmt.Sprintf("could not parse project: %s", err)
			fmt.Println(errmsg)
			s.ChannelMessageSend(StaffBotSpamChannelID, errmsg)
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

// urlRegex should match URLs in Discord messages. Exclude * for **bold** URLs.
var urlRegex *regexp.Regexp = regexp.MustCompile(`https?://[^\s*]+`)

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
			due, err = time.Parse("Due January 2", parts[0])
			if err != nil {
				innerErr = err
				return
			}
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

type hostResponse struct {
	// Message to show in #music-team
	Message string
	// Name of proposed piece
	Name string
	// Channel name
	Slug string
	// Raw response, pairs of question, answer
	Response [][]string
}

// checkHostResponses queries Google Sheets for new host responses.
func checkHostResponses(sheetsService *sheets.Service) ([]hostResponse, error) {
	stateRes, err := sheetsService.Spreadsheets.Values.Get(HostResponsesSheetID, HostResponsesBotSheet+"!B3:B3").Do()
	if err != nil {
		return nil, fmt.Errorf("could not query host responses sheet for bot state: %w", err)
	}
	id, err := strconv.Atoi(stateRes.Values[0][0].(string))
	if err != nil {
		return nil, fmt.Errorf("invalid last row id: %w", err)
	}
	res, err := sheetsService.Spreadsheets.Values.Get(HostResponsesSheetID, fmt.Sprintf("%s!A%d:M", HostResponsesSheet, id)).ValueRenderOption("UNFORMATTED_VALUE").Do()
	if err != nil {
		return nil, fmt.Errorf("could not query host responses sheet: %w", err)
	}
	var titles []string
	if len(res.Values) > 0 {
		rb := &sheets.ValueRange{
			Values: [][]interface{}{{id + len(res.Values)}},
		}
		_, err = sheetsService.Spreadsheets.Values.Update(HostResponsesSheetID, HostResponsesBotSheet+"!B3:B3", rb).ValueInputOption("RAW").Do()
		if err != nil {
			return nil, fmt.Errorf("could not update last row id: %w", err)
		}

		// get column titles for embed formatting
		titlesRes, err := sheetsService.Spreadsheets.Values.Get(HostResponsesSheetID, fmt.Sprintf("%s!A1:M", HostResponsesSheet)).ValueRenderOption("UNFORMATTED_VALUE").Do()
		if err != nil {
			return nil, fmt.Errorf("could not get column titles from host responses sheet: %w", err)
		}
		for _, title := range titlesRes.Values[0] {
			titles = append(titles, fmt.Sprintf("%s", title))
		}
	}
	var responses []hostResponse
	for _, row := range res.Values {
		strRow := make([]string, 0, len(row))
		for _, val := range row {
			strRow = append(strRow, fmt.Sprintf("%s", val))
		}
		strRow[0] = fmt.Sprintf("<t:%d:f>", convertSheetsDate(row[0].(float64)).Unix())
		responses = append(responses, hostResponse{
			Message:  fmt.Sprintf("**%s** proposes *%s*", row[1], row[8]),
			Name:     strRow[8],
			Slug:     slugify(strRow[8]),
			Response: zip(titles, strRow),
		})
	}
	return responses, nil
}

func convertSheetsDate(date float64) time.Time {
	i, f := math.Modf(date)
	t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return t.AddDate(0, 0, int(i)).Add(time.Duration(float64(24*time.Hour) * f))
}

var (
	slugRemoveCharacters = regexp.MustCompile(`[^-a-z0-9 ]`)
	slugSpace            = regexp.MustCompile(`[- ]+`)
)

// slugify makes an appropriate channel name.
func slugify(name string) string {
	name = strings.ToLower(name)
	name = slugRemoveCharacters.ReplaceAllString(name, "")
	name = slugSpace.ReplaceAllString(name, "-")
	return name
}

func zip[T any](a, b []T) [][]T {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	res := make([][]T, 0, l)
	for i := 0; i < l; i++ {
		res = append(res, []T{a[i], b[i]})
	}
	return res
}
