package main

import (
	"context"
	"fmt"
	"github.com/jellydator/ttlcache/v3"
	"github.com/sashabaranov/go-openai"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tg "github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jub0bs/fcors"
	"github.com/oybek/jethouse/db"
	"github.com/oybek/jethouse/telegram"
)

type Config struct {
	mdb           db.Config
	tgbotApiToken string
	openAiToken   string
}

func main() {
	log.SetOutput(os.Stdout)

	cfg := Config{
		mdb:           db.Config{Url: os.Getenv("ME_CONFIG_MONGODB_URL")},
		tgbotApiToken: os.Getenv("TG_BOT_API_TOKEN"),
		openAiToken:   os.Getenv("OPEN_AI_TOKEN"),
	}

	mongoClient, err := db.Create(cfg.mdb)
	if err != nil {
		log.Fatalf("Could not set up database: %v", err)
	}
	defer mongoClient.Disconnect(context.Background())

	//
	botOpts := tg.BotOpts{
		BotClient: &tg.BaseBotClient{
			Client: http.Client{},
			DefaultRequestOpts: &tg.RequestOpts{
				Timeout: 10 * time.Second,
				APIURL:  tg.DefaultAPIURL,
			},
		},
	}
	bot, err := tg.NewBot(cfg.tgbotApiToken, &botOpts)
	if err != nil {
		panic("failed to create new bot: " + err.Error())
	}

	openaiClient := openai.NewClient(cfg.openAiToken)

	photoCache := ttlcache.New(
		ttlcache.WithTTL[int64, []uuid.UUID](10*time.Minute),
		ttlcache.WithDisableTouchOnHit[int64, []uuid.UUID](),
	)

	longPoll := telegram.NewLongPoll(bot, mongoClient, openaiClient, photoCache)
	go longPoll.Run()

	cors, _ := fcors.AllowAccess(
		fcors.FromAnyOrigin(),
		fcors.WithMethods(
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
		),
		fcors.WithRequestHeaders("Authorization"),
	)

	r := mux.NewRouter()
	r.HandleFunc("/request/{uuid}", http.HandlerFunc(longPoll.GetRequest))
	http.Handle("/", cors(r))
	go http.ListenAndServe(":5556", nil)

	// listen for ctrl+c signal from terminal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	log.Println(fmt.Sprint(<-ch))
	log.Println("Stopping the bot...")
}
