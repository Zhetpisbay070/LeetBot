package telegram

import (
	"io"
	"log"
	"net/http"
	"os"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/google/uuid"
	"github.com/jellydator/ttlcache/v3"
)

func (lp *LongPoll) handlePhoto(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveMessage.Chat
	photos := ctx.EffectiveMessage.Photo

	if len(photos) == 0 {
		return nil
	}

	largestPhoto := photos[len(photos)-1]
	photoId, err := lp.savePhoto(&largestPhoto)
	if err != nil {
		return err
	}

	kv := lp.photoCache.Get(chat.Id)
	if kv != nil {
		lp.photoCache.Set(chat.Id, append(kv.Value(), photoId), ttlcache.DefaultTTL)
	} else {
		lp.photoCache.Set(chat.Id, []uuid.UUID{photoId}, ttlcache.DefaultTTL)
	}
	log.Printf("created: %#v", kv)

	return nil
}

func (lp *LongPoll) savePhoto(photo *gotgbot.PhotoSize) (photoId uuid.UUID, err error) {
	file0, err := lp.bot.GetFile(photo.FileId, &gotgbot.GetFileOpts{})
	if err != nil {
		return
	}

	resp, err := http.Get(file0.URL(lp.bot, &gotgbot.RequestOpts{}))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	photoId = uuid.New()
	file1, err := os.Create("photos/" + photoId.String() + ".jpg")
	if err != nil {
		return
	}
	//defer file1.Close()

	// Write the reader's content to the file
	_, err = io.Copy(file1, resp.Body)
	if err != nil {
		return
	}

	return
}
