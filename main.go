package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
	"google.golang.org/api/youtube/v3"
)

var yt *youtube.Service
var sheetsService *sheets.Service

// InitGoogle initializes the YouTube and Sheets API clients.
func InitGoogle(key string) error {
	var err error
	yt, err = youtube.NewService(context.TODO(), option.WithAPIKey(key))
	if err != nil {
		return err
	}
	data, err := ioutil.ReadFile("google_key.json")
	if err != nil {
		return fmt.Errorf("could not retrieve google_key.json: %w", err)
	}
	conf, err := google.JWTConfigFromJSON(data, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		return fmt.Errorf("could initialize service client: %w", err)
	}
	client := conf.Client(oauth2.NoContext)
	sheetsService, err = sheets.NewService(context.TODO(), option.WithHTTPClient(client))
	return err
}

func usage() {
	fmt.Printf("Usage: %s <command>\n", os.Args[0])
	fmt.Println("Commands:")
	fmt.Println(" - bot: start the Discord bot")
	fmt.Println(" - get-current-projects")
	fmt.Println(" - check-projects")
	fmt.Println(" - check-releases")
	fmt.Println(" - check-host-responses")
	fmt.Println(" - get-website-projects")
}

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	youtubeKey := os.Getenv("GOOGLE_API_KEY")

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	if youtubeKey != "" {
		if err := InitGoogle(youtubeKey); err != nil {
			fmt.Println("creating Google API client failed:", err)
			return
		}
	}

	cmd := os.Args[1]
	switch cmd {
	case "bot":
		if yt == nil {
			fmt.Println("need a YouTube client!")
			return
		}
		dg, err := InitBot(token, true)
		if err != nil {
			fmt.Println("error creating Discord session,", err)
			return
		}
		defer dg.Close()

		c := InitCron(dg)
		defer c.Stop()

		// Wait here until CTRL-C or other term signal is received.
		fmt.Println("Bot is now running.  Press CTRL-C to exit.")
		sc := make(chan os.Signal, 1)
		signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
		<-sc

	// Commands that need a discord session.
	case "get-current-projects",
		"check-projects":
		dg, err := InitBot(token, false)
		if err != nil {
			fmt.Println("error creating Discord session,", err)
			return
		}
		defer dg.Close()

		res, err := handleCommand("!"+cmd, dg)
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
		fmt.Println(res)

	// Commands that need a YouTube client
	case "check-releases",
		"check-host-responses":
		dg, err := InitBot(token, false)
		if err != nil {
			fmt.Println("error creating Discord session,", err)
			return
		}
		defer dg.Close()

		if yt == nil {
			fmt.Println("need a YouTube client!")
			return
		}

		res, err := handleCommand("!"+cmd, dg)
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
		fmt.Println(res)

	// Other commands
	case "get-website-projects":
		res, err := handleCommand("!"+cmd, nil)
		if err != nil {
			fmt.Println("error: ", err)
			return
		}
		fmt.Println(res)

	default:
		fmt.Printf("Unknown command %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

// handleCommand handles a bot command, returning the reply.
func handleCommand(cmd string, dg *discordgo.Session) (string, error) {
	switch cmd {
	case "!get-current-projects":
		projects, err := getCurrentProjects(dg, UVEGuildID)
		if err != nil {
			return "", err
		}
		var msg strings.Builder
		for _, project := range projects {
			fmt.Fprintf(&msg, "- %s due %s\n", project.ID, project.Deadline.Format("2006-01-02"))
		}
		return msg.String(), nil

	case "!get-website-projects":
		projects, err := getWebsiteProjects()
		if err != nil {
			return "", err
		}
		var msg strings.Builder
		for _, project := range projects {
			fmt.Fprintf(&msg, "- %s due %s\n", project.ID, project.Deadline.Format("2006-01-02"))
		}
		return msg.String(), nil

	case "!check-projects":
		res, err := checkCurrentProjects(dg, UVEGuildID)
		if err != nil {
			return "", err
		}
		if res == "" {
			res = "All good!"
		}
		return res, nil

	case "!check-releases":
		if yt == nil {
			return "", fmt.Errorf("no YouTube credentials supplied")
		}
		res, err := checkReleases(yt)
		if err != nil {
			return "", err
		}
		if res == "" {
			res = "All good!"
		}
		return res, nil

	case "!check-host-responses":
		if sheetsService == nil {
			return "", fmt.Errorf("no Google Sheets credentials supplied")
		}
		responses, err := checkHostResponses(sheetsService)
		if err != nil {
			return "", err
		}
		res := ""
		for _, response := range responses {
			channel, err := createProposedProjectChannel(dg, &response)
			if err != nil {
				return "", err
			}
			res += response.Message + fmt.Sprintf(" <#%s>\n", channel.ID)
		}
		return res, nil
	}
	return "", fmt.Errorf("unknown command %s", cmd)
}
