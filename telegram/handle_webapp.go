package telegram

import (
	"context"
	"log"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/oybek/jethouse/db"
	"github.com/oybek/jethouse/model"
)

func (lp *LongPoll) handleWebAppData(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := &ctx.EffectiveMessage.Chat

	webAppData := ctx.EffectiveMessage.WebAppData
	if webAppData != nil {
		lp.bot.DeleteMessage(chat.Id, ctx.EffectiveMessage.MessageId, &gotgbot.DeleteMessageOpts{})
	}
	if strings.HasPrefix(ctx.EffectiveMessage.Text, "/webapp") {
		webAppData = &gotgbot.WebAppData{
			Data: strings.TrimPrefix(ctx.EffectiveMessage.Text, "/webapp"),
		}
	}
	if webAppData == nil {
		return nil
	}

	json := webAppData.Data
	log.Printf("[ChatId=%d] Got json from WebApp: %s", chat.Id, json)

	if house, err := model.ParseAndValidate[model.House](json); err == nil {
		//house.OwnerID = chat.Id
		//house.Active = true
		//kv, _ := lp.photoCache.GetAndDelete(chat.Id)
		//if kv != nil {
		//	house.PhotoIDs = kv.Value()
		//}
		return lp.handleWebAppHouse(chat, house)
	}

	return lp.sendText(chat.Id, "Что-то пошло не так - попробуйте еще раз")
}

func (lp *LongPoll) handleWebAppHouse(chat *gotgbot.Chat, house *model.House) error {
	coll := lp.mongoClient.Database(db.Database).Collection("houses")
	_, err := coll.InsertOne(context.Background(), house)
	if err != nil {
		return err
	}

	lp.sendText(chat.Id, "Объявление успешно создано!")

	return nil
}
