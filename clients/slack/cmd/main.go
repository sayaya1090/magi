package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/sayaya1090/magi/clients/appbridge"
	slackclient "github.com/sayaya1090/magi/clients/slack"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	appToken, botToken := os.Getenv("SLACK_APP_TOKEN"), os.Getenv("SLACK_BOT_TOKEN")
	if appToken == "" || botToken == "" {
		log.Fatal("SLACK_APP_TOKEN and SLACK_BOT_TOKEN are required")
	}
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	auth, err := api.AuthTestContext(ctx)
	if err != nil {
		log.Fatal("Slack authentication failed; verify app installation and bot token")
	}
	var client *slackclient.Client
	bridge, err := appbridge.New(appbridge.Config{App: "slack", Addr: "127.0.0.1:8736", Instructions: slackclient.Instructions, Tools: slackclient.Tools(), Call: func(ctx context.Context, target, name string, args json.RawMessage) (any, error) {
		return client.Call(ctx, target, name, args)
	}})
	if err != nil {
		log.Fatal("Magi Slack bridge initialization failed")
	}
	client = slackclient.New(bridge, api, auth.UserID)
	go func() {
		if err := bridge.Run(ctx); err != nil && ctx.Err() == nil {
			log.Print("Magi Slack bridge stopped")
			cancel()
		}
	}()
	socket := socketmode.New(api)
	go func() {
		if err := socket.RunContext(ctx); err != nil && ctx.Err() == nil {
			log.Print("Slack connection stopped")
			cancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-socket.Events:
			if ev.Request != nil {
				socket.Ack(*ev.Request)
			}
			switch ev.Type {
			case socketmode.EventTypeEventsAPI:
				outer, ok := ev.Data.(slackevents.EventsAPIEvent)
				if !ok || outer.TeamID != auth.TeamID {
					continue
				}
				eventID := ""
				if callback, ok := outer.Data.(*slackevents.EventsAPICallbackEvent); ok {
					eventID = callback.EventID
				}
				switch e := outer.InnerEvent.Data.(type) {
				case *slackevents.AppHomeOpenedEvent:
					if e.Tab == "home" {
						go func() {
							if err := client.Home(ctx, e.User); err != nil {
								log.Print("Slack Home publication failed")
							}
						}()
					}
				case *slackevents.AppMentionEvent:
					if e.BotID != "" || e.Edited != nil {
						continue
					}
					thread := e.ThreadTimeStamp
					if thread == "" {
						thread = e.TimeStamp
					}
					i := slackclient.Input{EventID: eventID, Team: outer.TeamID, User: e.User, Channel: e.Channel, Thread: thread, Text: e.Text}
					go func() {
						if err := client.Handle(ctx, i); err != nil {
							client.Notify(ctx, i, err.Error())
							log.Print("Slack request delivery failed")
						}
					}()
				case *slackevents.MessageEvent:
					if e.ChannelType != "im" || e.BotID != "" || e.SubType != "" {
						continue
					}
					thread := e.ThreadTimeStamp
					if thread == "" {
						thread = e.TimeStamp
					}
					i := slackclient.Input{EventID: eventID, Team: outer.TeamID, User: e.User, Channel: e.Channel, Thread: thread, Text: e.Text}
					go func() {
						if err := client.Handle(ctx, i); err != nil {
							client.Notify(ctx, i, err.Error())
							log.Print("Slack request delivery failed")
						}
					}()
				}
			case socketmode.EventTypeInteractive:
				cb, ok := ev.Data.(slack.InteractionCallback)
				if !ok || cb.Team.ID != auth.TeamID {
					continue
				}
				for _, a := range cb.ActionCallback.BlockActions {
					if a.ActionID != "magi_answer" {
						continue
					}
					i := slackclient.Input{Team: cb.Team.ID, User: cb.User.ID, Channel: cb.Channel.ID, Thread: cb.Message.ThreadTimestamp}
					if i.Thread == "" {
						i.Thread = cb.Message.Timestamp
					}
					if client.Action(ctx, i, a.Value) != nil {
						log.Print("Slack action rejected or delivery failed")
					}
				}
			}
		}
	}
}
