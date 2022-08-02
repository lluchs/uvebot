package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
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

	dg, err := InitBot(token)
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
}
