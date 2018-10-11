package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"errors"
	"strconv"

	"github.com/ChimeraCoder/anaconda"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"

	_ "github.com/go-sql-driver/mysql" // MySQL Driver

	"github.com/golang-migrate/migrate"
	"github.com/golang-migrate/migrate/database/mysql"
	_ "github.com/golang-migrate/migrate/source/file"

	"github.com/dankeroni/gotwitch"
	"github.com/gempir/go-twitch-irc"
	"github.com/pajlada/go-twitch-pubsub"
	"github.com/pajlada/pajbot2/emotes"
	"github.com/pajlada/pajbot2/pkg"
	"github.com/pajlada/pajbot2/pkg/apirequest"
	"github.com/pajlada/pajbot2/pkg/commands"
	"github.com/pajlada/pajbot2/pkg/common/config"
	"github.com/pajlada/pajbot2/pkg/modules"
	"github.com/pajlada/pajbot2/pkg/pubsub"
	"github.com/pajlada/pajbot2/pkg/report"
	pb2twitch "github.com/pajlada/pajbot2/pkg/twitch"
	"github.com/pajlada/pajbot2/pkg/users"
	"github.com/pajlada/pajbot2/web"
)

type channelContext struct {
	// TODO: replace []string with some 5 message long fifo queue
	Channels map[string][]string
}

func NewChannelContext() *channelContext {
	return &channelContext{
		Channels: make(map[string][]string),
	}
}

// Application is the heart of pajbot
// It keeps the functions to initialize, start, and stop pajbot
type Application struct {
	config *config.Config

	TwitchBots   map[string]*pb2twitch.Bot
	Redis        *redis.Pool
	SQL          *sql.DB
	Twitter      *twitter.Client
	TwitchPubSub *twitchpubsub.Client

	ReportHolder *report.Holder

	// key = user ID
	UserContext map[string]*channelContext

	Quit chan string

	PubSub            *pubsub.PubSub
	TwitchUserStore   pkg.UserStore
	TwitchUserContext pkg.UserContext
}

func lol(xd string) *string {
	return &xd
}

func (a *Application) GetUserMessages(channelID, userID string) ([]string, error) {
	if uc, ok := a.UserContext[userID]; ok {
		if cc, ok := uc.Channels[channelID]; ok {
			return cc, nil
		}

		return nil, errors.New("No messages found in this channel for this user")
	}

	return nil, errors.New("No messages found for this user")
}

// NewApplication creates an instance of Application. Generally this should only be done once
func NewApplication() *Application {
	a := Application{}

	a.TwitchUserStore = NewUserStore()
	a.TwitchUserContext = NewUserContext()

	a.TwitchBots = make(map[string]*pb2twitch.Bot)
	a.Quit = make(chan string)
	a.UserContext = make(map[string]*channelContext)
	a.PubSub = pubsub.New()

	go a.PubSub.Run()

	return &a
}

// LoadConfig loads a config file from the given path. The format for the config file is available in the config package
func (a *Application) LoadConfig(path string) error {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return err
	}

	a.config = cfg

	return nil
}

// RunDatabaseMigrations runs database migrations on the database specified in the config file
func (a *Application) RunDatabaseMigrations() error {
	db, err := sql.Open("mysql", a.config.SQL.DSN)
	if err != nil {
		return err
	}

	defer func() {
		dErr := db.Close()
		if dErr != nil {
			fmt.Println("Error in deferred close:", dErr)
		}
	}()

	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithDatabaseInstance("file://migrations", "mysql", driver)
	if err != nil {
		return err
	}

	err = m.Up()

	if err != nil {
		if err == migrate.ErrNoChange {
			return nil
		}

		return err
	}

	return nil
}

func onHTTPError(statusCode int, statusMessage, errorMessage string) {
	fmt.Println("HTTPERROR: ", errorMessage)
}

func onInternalError(err error) {
	fmt.Printf("internal error: %s", err)
}

// InitializeAPIs initializes various APIs that are needed for pajbot
func (a *Application) InitializeAPIs() error {
	// Twitch APIs
	apirequest.Twitch = gotwitch.New(a.config.Auth.Twitch.User.ClientID)
	apirequest.TwitchBot = gotwitch.New(a.config.Auth.Twitch.Bot.ClientID)
	apirequest.TwitchV3 = gotwitch.NewV3(a.config.Auth.Twitch.User.ClientID)
	apirequest.TwitchBotV3 = gotwitch.NewV3(a.config.Auth.Twitch.Bot.ClientID)

	// onSuccess := func(data []gotwitch.User) {
	// 	fmt.Printf("%#v\n", data)
	// }

	// apirequest.Twitch.GetUsersByLogin([]string{"bajlada"}, onSuccess, onHTTPError, onInternalError)

	/*
		apirequest.Twitch.SubscribeFollows("19571641", "http://57552418.ngrok.io/api/callbacks/follow", func() {
			fmt.Println("success")
		}, func() {
			fmt.Println("error")
		})
	*/

	apirequest.Twitch.SubscribeStreams("159849156", "http://57552418.ngrok.io/api/callbacks/streams", func() {
		fmt.Println("streams success")
	}, func() {
		fmt.Println("streams error")
	})

	apirequest.Twitch.SubscribeStreams("11148817", "http://57552418.ngrok.io/api/callbacks/streams", func() {
		fmt.Println("streams success")
	}, func() {
		fmt.Println("streams error")
	})

	return nil
}

// LoadExternalEmotes xd
func (a *Application) LoadExternalEmotes() error {
	fmt.Println("Loading globalemotes...")
	go emotes.LoadGlobalEmotes()
	fmt.Println("Done!")

	return nil
}

func (a *Application) StartRedisClient() error {
	a.Redis = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", a.config.Redis.Host)
			if err != nil {
				log.Fatal("An error occured while connecting to redis: ", err)
				return nil, err
			}
			if a.config.Redis.Database >= 0 {
				_, err = c.Do("SELECT", a.config.Redis.Database)
				if err != nil {
					log.Fatal("Error while selecting redis db:", err)
					return nil, err
				}
			}
			return c, err
		},
	}

	// Ensure that the redis connection works
	conn := a.Redis.Get()
	return conn.Send("PING")
}

func (a *Application) StartSQLClient() error {
	var err error
	a.SQL, err = sql.Open("mysql", a.config.SQL.DSN)
	if err != nil {
		return err
	}

	// TODO: move this to init
	a.ReportHolder, err = report.New(a.SQL, a.PubSub, a.TwitchUserStore)
	if err != nil {
		return err
	}

	return nil
}

func (a *Application) StartTwitterStream() error {
	localConfig := a.config.Auth.Twitter

	// users lookup
	// userLookupParams := &twitter.UserLookupParams{ScreenName: []string{"pajtest"}}
	// users, _, _ := client.Users.Lookup(userLookupParams)
	// fmt.Printf("USERS LOOKUP:\n%+v\n", users)

	if localConfig.ConsumerKey == "" || localConfig.ConsumerSecret == "" || localConfig.AccessToken == "" || localConfig.AccessSecret == "" {
		return errors.New("Missing twitter configuration fields")
	}

	api := anaconda.NewTwitterApiWithCredentials(localConfig.AccessToken, localConfig.AccessSecret, localConfig.ConsumerKey, localConfig.ConsumerSecret)

	v := url.Values{}
	s := api.UserStream(v)

	for t := range s.C {
		fmt.Printf("%#v\n", t)
		switch v := t.(type) {
		case anaconda.Tweet:
			fmt.Printf("%-15s: %s\n", v.User.ScreenName, v.Text)
		case anaconda.EventTweet:
			switch v.Event.Event {
			case "favorite":
				sn := v.Source.ScreenName
				tw := v.TargetObject.Text
				fmt.Printf("Favorited by %-15s: %s\n", sn, tw)
			case "unfavorite":
				sn := v.Source.ScreenName
				tw := v.TargetObject.Text
				fmt.Printf("UnFavorited by %-15s: %s\n", sn, tw)
			}
		}
	}

	/*
		config := oauth1.NewConfig(localConfig.ConsumerKey, localConfig.ConsumerSecret)
		token := oauth1.NewToken(localConfig.AccessToken, localConfig.AccessSecret)

		httpClient := config.Client(oauth1.NoContext, token)
		client := twitter.NewClient(httpClient)

		demux := twitter.NewSwitchDemux()
		demux.All = func(x interface{}) {
			fmt.Printf("x %#v\n", x)
		}
		demux.StreamDisconnect = func(disconnect *twitter.StreamDisconnect) {
			fmt.Printf("disconnected %#v\n", disconnect)
		}
		demux.Tweet = func(tweet *twitter.Tweet) {
			fmt.Println(tweet.Text)
		}

		demux.Event = func(event *twitter.Event) {
			fmt.Printf("%#v\n", event)
		}

		filterParams := &twitter.StreamFilterParams{
			// Follow:        []string{"81085011"},
			// Track: []string{"cat"},
			// StallWarnings: twitter.Bool(true),
		}
		stream, err := client.Streams.Filter(filterParams)
		if err != nil {
			return err
		}

		fmt.Printf("stream is %#v\n", stream)
		fmt.Printf("messages is is %#v\n", stream.Messages)

		fmt.Println("start handling..")
		for message := range stream.Messages {
			fmt.Printf("got message %#v\n", message)
			// demux.Handle(message)
		}
		_, xd := (<-stream.Messages)
		if xd {
			fmt.Println("channel is not closed")
		}
		fmt.Printf("messages is is %#v\n", stream.Messages)
		fmt.Println("done")
	*/

	return nil
}

// StartWebServer starts the web server associated to the bot
func (a *Application) StartWebServer() error {
	webCfg := &web.Config{
		Redis: a.Redis,
		SQL:   a.SQL,
	}

	webBoss := web.Init(a.config, webCfg, a.PubSub, a.TwitchUserStore)
	go webBoss.Run()

	return nil
}

type UnicodeRange struct {
	Start rune
	End   rune
}

func checkModules(next pb2twitch.Handler) pb2twitch.Handler {
	return pb2twitch.HandlerFunc(func(bot *pb2twitch.Bot, channel pkg.Channel, user pkg.User, message *pb2twitch.TwitchMessage, action pkg.Action) {
		modulesStart := time.Now()
		defer func() {
			modulesEnd := time.Now()

			if pkg.VerboseBenchmark {
				fmt.Printf("[% 26s] %s", "Total", modulesEnd.Sub(modulesStart))
			}
		}()

		for _, module := range bot.Modules {
			moduleStart := time.Now()
			var err error
			if channel == nil {
				err = module.OnWhisper(bot, user, message)
			} else {
				err = module.OnMessage(bot, channel, user, message, action)
			}
			moduleEnd := time.Now()
			if pkg.VerboseBenchmark {
				fmt.Printf("[% 26s] %s", module.Name(), moduleEnd.Sub(moduleStart))
			}
			if err != nil {
				fmt.Printf("%s: %s\n", module.Name(), err)
			}
		}

		next.HandleMessage(bot, channel, user, message, action)
	})
}

type messageReceivedData struct {
	Sender  string
	Message string
}

func (a *Application) notifyPubSub(next pb2twitch.Handler) pb2twitch.Handler {
	return pb2twitch.HandlerFunc(func(bot *pb2twitch.Bot, channel pkg.Channel, user pkg.User, message *pb2twitch.TwitchMessage, action pkg.Action) {
		a.PubSub.Publish("MessageReceived", &messageReceivedData{
			Sender:  user.GetName(),
			Message: message.Text,
		}, pkg.PubSubAdminAuth())
		next.HandleMessage(bot, channel, user, message, action)
	})
}

func (a *Application) storeContext(next pb2twitch.Handler) pb2twitch.Handler {
	return pb2twitch.HandlerFunc(func(bot *pb2twitch.Bot, channel pkg.Channel, user pkg.User, message *pb2twitch.TwitchMessage, action pkg.Action) {
		if channel != nil && user != nil {
			formattedMessage := fmt.Sprintf("[%s] %s: %s", time.Now().Format("15:04:05"), user.GetName(), message.GetText())
			a.TwitchUserContext.AddContext(channel.GetID(), user.GetID(), formattedMessage)
		}

		next.HandleMessage(bot, channel, user, message, action)
	})
}

// LoadBots loads bots from the database
func (a *Application) LoadBots() error {
	db, err := sql.Open("mysql", a.config.SQL.DSN)
	if err != nil {
		return err
	}

	defer func() {
		dErr := db.Close()
		if dErr != nil {
			fmt.Println("Error in deferred close:", dErr)
		}
	}()

	rows, err := db.Query("SELECT `name`, `twitch_access_token` FROM `pb_bot`")
	if err != nil {
		return err
	}

	defer func() {
		dErr := rows.Close()
		if dErr != nil {
			fmt.Println("Error in deferred rows close:", dErr)
		}
	}()

	/*
	 Sorry :( To prevent racism we only allow basic Latin Letters with some exceptions. If you think your message should not have been timed out, please send a link to YOUR chatlogs for the MONTH with a TIMESTAMP of the offending message to "omgscoods@gmail.com" and we'll review it.
	*/

	err = modules.InitServer(a.Redis, a.SQL, a.config.Pajbot1, a.PubSub)
	if err != nil {
		return err
	}

	err = users.InitServer(a.SQL)
	if err != nil {
		return err
	}

	for rows.Next() {
		var name string
		var twitchAccessToken string
		if err := rows.Scan(&name, &twitchAccessToken); err != nil {
			return err
		}

		if strings.HasPrefix(twitchAccessToken, "oauth:") {
			panic(fmt.Sprintf("Twitch access token for bot %s must not start with oauth: prefix", name))
		}

		finalHandler := pb2twitch.HandlerFunc(pb2twitch.FinalMiddleware)

		bot := pb2twitch.NewBot(twitch.NewClient(name, "oauth:"+twitchAccessToken), a.PubSub, a.TwitchUserStore, a.TwitchUserContext)
		bot.Name = name
		bot.QuitChannel = a.Quit

		// Parsing
		bot.AddModule(modules.NewBTTVEmoteParser(&emotes.GlobalEmotes.Bttv))

		// Report module/Admin commands
		bot.AddModule(modules.NewReportModule(a.ReportHolder))

		// Filtering
		bot.AddModule(modules.NewBadCharacterFilter())
		bot.AddModule(modules.NewLatinFilter())
		bot.AddModule(modules.NewPajbot1BanphraseFilter())
		bot.AddModule(modules.NewEmoteFilter(bot))
		bot.AddModule(modules.NewBannedNames())
		bot.AddModule(modules.NewLinkFilter())

		bot.AddModule(modules.NewMessageLengthLimit())

		// Actions
		bot.AddModule(modules.NewActionPerformer())

		// Commands
		bot.AddModule(modules.NewPajbot1Commands(bot))

		customCommands := modules.NewCustomCommands()
		customCommands.RegisterCommand([]string{"!userid"}, &commands.GetUserID{})
		customCommands.RegisterCommand([]string{"!username"}, &commands.GetUserName{})
		customCommands.RegisterCommand([]string{"!pb2points"}, &commands.GetPoints{})
		customCommands.RegisterCommand([]string{"!pb2roulette"}, &commands.Roulette{})
		customCommands.RegisterCommand([]string{"!pb2givepoints"}, &commands.GivePoints{})
		// customCommands.RegisterCommand([]string{"!pb2addpoints"}, &commands.AddPoints{})
		// customCommands.RegisterCommand([]string{"!pb2removepoints"}, &commands.RemovePoints{})
		customCommands.RegisterCommand([]string{"!roffle", "!join"}, commands.NewRaffle())
		customCommands.RegisterCommand([]string{"!user"}, commands.NewUser())
		customCommands.RegisterCommand([]string{"!pb2rank"}, &commands.Rank{})
		customCommands.RegisterCommand([]string{"!pb2ping"}, &commands.Ping{})
		customCommands.RegisterCommand([]string{"!pb2simplify"}, &commands.Simplify{})
		// customCommands.RegisterCommand([]string{"!timemeout"}, &commands.TimeMeOut{})
		customCommands.RegisterCommand([]string{"!pb2test"}, &commands.Test{})

		bot.AddModule(customCommands)

		bot.AddModule(modules.NewGiveaway(bot))

		// Moderation
		bot.AddModule(modules.NewNuke())

		bot.SetHandler(a.storeContext(a.notifyPubSub(checkModules(pb2twitch.HandleCommands(finalHandler)))))

		a.TwitchBots[name] = bot
	}

	return nil
}

// StartBots starts bots that were loaded from the LoadBots method
func (a *Application) StartBots() error {
	for _, bot := range a.TwitchBots {
		go func(bot *pb2twitch.Bot) {
			if bot.Name != "snusbot" {
				// continue
			}

			bot.OnNewWhisper(bot.HandleWhisper)

			bot.OnNewMessage(bot.HandleMessage)

			bot.OnNewRoomstateMessage(bot.HandleRoomstateMessage)

			if bot.Name == "gempir" {
				bot.Join("pajlada")
			}

			if bot.Name == "snusbot" {
				bot.Join("forsen")
			}

			if bot.Name == "botnextdoor" {
				bot.Join("nymn")
			}

			if bot.Name == "pajbot" {
				bot.Join("krakenbul")
				bot.Join("nani")
				bot.Join("pajlada")
				bot.Join("narwhal_dave")
				// err := bot.ConnectToPointServer()
				// if err != nil {
				// 	log.Fatal(err)
				// }
				// bot.StartChatterPoller()
			}

			bot.Join(bot.Name)

			err := bot.Connect()
			if err != nil {
				log.Fatal(err)
			}
		}(bot)
	}

	return nil
}

func (a *Application) StartPubSubClient() error {
	cfg := &a.config.PubSub
	a.TwitchPubSub = twitchpubsub.NewClient()

	err := a.TwitchPubSub.Connect()
	if err != nil {
		return err
	}

	if cfg.ChannelID == "" || cfg.UserID == "" || cfg.UserToken == "" {
		return errors.New("Missing PubSub configuration stuff")
	}

	return a.listenToModeratorActions(cfg.UserID, cfg.ChannelID, cfg.UserToken)
}

func (a *Application) listenToModeratorActions(userID, channelID, userToken string) error {
	moderationTopic := twitchpubsub.ModerationActionTopic(userID, channelID)
	a.TwitchPubSub.Listen(moderationTopic, userToken, func(bytes []byte) error {
		event, err := twitchpubsub.GetModerationAction(bytes)
		if err != nil {
			return err
		}

		const ActionUnknown = 0
		const ActionTimeout = 1
		const ActionBan = 2
		const ActionUnban = 3
		duration := 0

		content := fmt.Sprintf("Moderation action: %+v", event)
		fmt.Println(content)
		var actionContext *string
		action := 0
		reason := ""
		const queryF = "INSERT INTO `ModerationAction` (ChannelID, UserID, Action, Duration, TargetID, Reason, Context) VALUES (?, ?, ?, ?, ?, ?, ?);"
		switch event.ModerationAction {
		case "timeout":
			action = ActionTimeout
			content = fmt.Sprintf("%s timed out %s for %s seconds", event.CreatedBy, event.Arguments[0], event.Arguments[1])
			duration, _ = strconv.Atoi(event.Arguments[1])
			if len(event.Arguments[2]) > 0 {
				reason = event.Arguments[2]
				content += " for reason: \"" + reason + "\""
			}
			msgs, err := a.GetUserMessages(channelID, event.TargetUserID)
			if err == nil {
				actionContext = lol(strings.Join(msgs, "\n"))
			}

			e := pkg.PubSubTimeoutEvent{
				Channel: pkg.PubSubUser{
					ID: channelID,
				},
				Target: pkg.PubSubUser{
					ID:   event.TargetUserID,
					Name: event.Arguments[0],
				},
				Source: pkg.PubSubUser{
					ID:   event.CreatedByUserID,
					Name: event.CreatedBy,
				},
				Duration: duration,
				Reason:   reason,
			}

			a.PubSub.Publish("TimeoutEvent", e, pkg.PubSubAdminAuth())

		case "ban":
			action = ActionBan
			content = fmt.Sprintf("%s banned %s", event.CreatedBy, event.Arguments[0])
			if len(event.Arguments[1]) > 0 {
				reason = event.Arguments[1]
				content += " for reason: \"" + reason + "\""
			}
			msgs, err := a.GetUserMessages(channelID, event.TargetUserID)
			if err == nil {
				actionContext = lol(strings.Join(msgs, "\n"))
			}

			e := pkg.PubSubBanEvent{
				Channel: pkg.PubSubUser{
					ID: channelID,
				},
				Target: pkg.PubSubUser{
					ID:   event.TargetUserID,
					Name: event.Arguments[0],
				},
				Source: pkg.PubSubUser{
					ID:   event.CreatedByUserID,
					Name: event.CreatedBy,
				},
				Reason: reason,
			}

			a.PubSub.Publish("BanEvent", e, pkg.PubSubAdminAuth())

		case "unban", "untimeout":
			action = ActionUnban
			content = fmt.Sprintf("%s unbanned %s", event.CreatedBy, event.Arguments[0])
		}

		if action != 0 {
			_, err := a.SQL.Exec(queryF, channelID, event.CreatedByUserID, action, duration, event.TargetUserID, reason, actionContext)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return nil
}

// Run blocks the current thread, waiting for something to put an exit string into the Quit channel
func (a *Application) Run() error {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)
	go func() {
		<-c
		a.Quit <- "Quitting due to SIGTERM/SIGINT"
	}()

	quitString := <-a.Quit

	return fmt.Errorf(quitString)
}
