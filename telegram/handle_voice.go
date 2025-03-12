package telegram

import (
	"context"
	"fmt"
	"net/http"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/sashabaranov/go-openai"
)

func (lp *LongPoll) handleVoice(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveMessage.Chat
	voice := ctx.EffectiveMessage.Voice

	if voice.Duration > 20 {
		return lp.sendText(chat.Id, TextTooLongVoice)
	}

	lp.sendText(chat.Id, "Ищу подходящие аптеки")

	_, err := lp.transcribeVoice(voice)
	if err != nil {
		return err
	}

	return nil
}

func (lp *LongPoll) transcribeVoice(voice *gotgbot.Voice) (string, error) {
	file, err := lp.bot.GetFile(voice.FileId, &gotgbot.GetFileOpts{})
	if err != nil {
		return "", err
	}

	resp, err := http.Get(file.URL(lp.bot, &gotgbot.RequestOpts{}))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	req := openai.AudioRequest{
		Model:    openai.Whisper1,
		Reader:   resp.Body,
		FilePath: file.FilePath,
		Prompt:   "Парацетамол, ТайлолХот, Тримол",
		Language: "ru",
	}

	context := context.Background()
	openaiResp, err := lp.openaiClient.CreateTranscription(context, req)
	if err != nil {
		fmt.Printf("Transcription error: %v\n", err)
		return "", err
	}

	return openaiResp.Text, nil
}
