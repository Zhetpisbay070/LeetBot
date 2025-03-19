package telegram

import (
	"context"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
	"github.com/google/uuid"
	"github.com/jellydator/ttlcache/v3"
	"github.com/oybek/jethouse/db"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

type LongPoll struct {
	bot           *gotgbot.Bot
	mongoClient   *mongo.Client
	openaiClient  *openai.Client
	photoCache    *ttlcache.Cache[int64, []uuid.UUID]
	prevProcesses map[int64]string
}

func NewLongPoll(
	bot *gotgbot.Bot,
	mongoClient *mongo.Client,
	openaiClient *openai.Client,
	photoCache *ttlcache.Cache[int64, []uuid.UUID],
) *LongPoll {
	return &LongPoll{
		bot:          bot,
		mongoClient:  mongoClient,
		openaiClient: openaiClient,
		photoCache:   photoCache,
	}
}

const createAptekaWebAppUrl = "https://wolfrepos.github.io/apteka/create/index.html"

func (lp *LongPoll) Run() {
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})
	updater := ext.NewUpdater(dispatcher, nil)

	// Setup handlers
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/start111") },
		lp.handleStartSession,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/buy") },
		lp.handleBuySubscription,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/close") },
		lp.handleEndOfSession,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/techsup") },
		lp.handleTechSupportCommand,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/feedback") },
		lp.handleBotFeedbackCommand,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		func(query *gotgbot.CallbackQuery) bool { return strings.HasPrefix(query.Data, "feedbackKeys") },
		lp.handlerFeedSelection,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		func(query *gotgbot.CallbackQuery) bool { return strings.HasPrefix(query.Data, "sub_") },
		lp.handleSubscriptionCallback,
	))
	dispatcher.AddHandler(handlers.NewCallback(
		func(query *gotgbot.CallbackQuery) bool { return strings.HasPrefix(query.Data, "prompt_") },
		lp.handlePromptSelection,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			return true //
		},
		lp.handleUserMessage,
	))

	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return strings.HasPrefix(msg.Text, "/create_apteka") },
		lp.handleCreateApteka,
	))
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool { return msg.WebAppData != nil },
		lp.handleWebAppData,
	))
	dispatcher.AddHandler(handlers.NewMessage(func(msg *gotgbot.Message) bool {
		return strings.HasPrefix(msg.Text, "/webapp")
	}, lp.handleWebAppData))
	dispatcher.AddHandler(handlers.NewMessage(message.Text, lp.handleText))
	dispatcher.AddHandler(handlers.NewMessage(message.Voice, lp.handleVoice))
	dispatcher.AddHandler(handlers.NewMessage(message.Photo, lp.handlePhoto))

	// Start receiving updates.
	err := updater.StartPolling(lp.bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
			},
		},
	})
	if err != nil {
		panic("failed to start polling: " + err.Error())
	}

	// Setup commands
	lp.bot.SetMyCommands(
		[]gotgbot.BotCommand{
			{Command: "create_apteka", Description: "Создать аптеку"},
		}, nil,
	)

	log.Printf("%s has been started...\n", lp.bot.User.Username)

	// Idle, to keep updates coming in, and avoid bot stopping.
	updater.Idle()
}

func (lp *LongPoll) handleStartSession(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id

	// Проверяем, можно ли запустить сессию
	allowed, _, err := lp.canStartSession(userID)
	if err != nil {
		return err
	}

	if !allowed {
		return lp.sendText(userID, "Вы не можете запустить сессию. Возможно, у вас нет подписки.")
	}

	keyboard := [][]gotgbot.InlineKeyboardButton{
		{gotgbot.InlineKeyboardButton{Text: "Тема 1", CallbackData: "prompt_1"}},
		{gotgbot.InlineKeyboardButton{Text: "Тема 2", CallbackData: "prompt_2"}},
		{gotgbot.InlineKeyboardButton{Text: "Тема 3", CallbackData: "prompt_3"}},
	}

	// Отправляем сообщение с кнопками сразу после /start111
	_, err = b.SendMessage(userID, "Выберите тему:", &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	})
	if err != nil {
		log.Println("Ошибка при отправке кнопок:", err)
		return err
	}

	// создаем сессию
	_, _, err = lp.getOrCreateSession(userID)
	if err != nil {
		log.Println("Ошибка при создании сессии:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	return nil
}

func (lp *LongPoll) handlerRequestSessionFeedback(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id
	sessionID, _, err := lp.findLastClosedSession(userID)
	if err != nil || sessionID == "" {
		log.Println("[handlerRequestSessionFeedback] Нет закрытых сессий для фидбека.")
		return nil
	}

	msg := "Оцените сессию от 1 до 5:"
	buttons := [][]gotgbot.InlineKeyboardButton{
		{
			{Text: "1", CallbackData: fmt.Sprintf("feedback_%s_1", sessionID)},
			{Text: "2", CallbackData: fmt.Sprintf("feedback_%s_2", sessionID)},
			{Text: "3", CallbackData: fmt.Sprintf("feedback_%s_3", sessionID)},
			{Text: "4", CallbackData: fmt.Sprintf("feedback_%s_4", sessionID)},
			{Text: "5", CallbackData: fmt.Sprintf("feedback_%s_5", sessionID)},
		},
	}
	//Отправляем смс с кнопками фидбека
	_, err = b.SendMessage(userID, msg, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{InlineKeyboard: buttons},
	})
	if err != nil {
		log.Println("Ошибка при отправке кнопок фидбека:", err)
	}

	return nil
}

func (lp *LongPoll) handlerFeedSelection(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery
	if query == nil {
		log.Println("[handlerFeedSelection] Пустой CallbackQuery, пропускаем обработку")
		return nil // Защита от пустых запросов
	}

	chatID := query.From.Id
	messageID := query.Message.GetMessageId()
	userID := query.From.Id
	dataParts := strings.Split(query.Data, "_") // Разбираем callback data

	log.Printf("[handlerFeedSelection] Получен callback-запрос от userID=%d, chatID=%d, messageID=%d, data=%s", userID, chatID, messageID, query.Data)

	if len(dataParts) != 3 {
		log.Println("Некорректный формат callback data:", query.Data)
		return nil
	}

	sessionID := dataParts[1]
	score := dataParts[2]

	log.Printf("[handlerFeedSelection] sessionID=%s, score=%s", sessionID, score)

	// Сохраняем фидбек в MongoDB
	err := lp.saveFeedbackSessionMessage(userID, sessionID, score)
	if err != nil {
		log.Println("Ошибка при сохранении фидбека:", err)
		return err
	}
	log.Println("[handlerFeedSelection] Фидбек успешно сохранен в MongoDB")

	// Подтверждаем клик по кнопке (чтобы "часики" исчезли)
	_, _ = query.Answer(b, nil)
	log.Println("[handlerFeedSelection] Callback-кнопка подтверждена (часики исчезли)")

	// Удаляем кнопки (редактируем сообщение, убирая markup)
	log.Printf("[handlerFeedSelection] Удаляем кнопки из сообщения chatID=%d, messageID=%d", chatID, messageID)
	_, _, err = b.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
		ChatId:    chatID,
		MessageId: messageID,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{}, // Убираем кнопки
		},
	})
	if err != nil {
		log.Println("Ошибка при удалении кнопок:", err)
	} else {
		log.Println("[handlerFeedSelection] Кнопки успешно удалены")
	}

	// Удаляем сообщение
	log.Printf("[handlerFeedSelection] Удаляем сообщение chatID=%d, messageID=%d", chatID, messageID)
	_, err = b.DeleteMessage(chatID, messageID, nil)
	if err != nil {
		log.Println("Ошибка при удалении сообщения:", err)
	} else {
		log.Println("[handlerFeedSelection] Сообщение успешно удалено")
	}

	return nil
}

func (lp *LongPoll) handlePromptSelection(b *gotgbot.Bot, ctx *ext.Context) error {
	query := ctx.CallbackQuery //TODO - разница между эффектив месседж
	userID := query.From.Id
	chatID := ctx.EffectiveChat.Id
	messageID := ctx.EffectiveMessage.MessageId
	promptID := query.Data

	sessionID, _, err := lp.getOrCreateSession(userID)
	if err != nil {
		log.Println("Ошибка при получении sessionID:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	//Подтверждаем нажатие кнопки
	_, _ = query.Answer(b, nil)

	//Удаляем кнопки после выбора промта
	_, _, err = b.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
		ChatId:      chatID,
		MessageId:   messageID,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{}, // Удаление кнопок
	})
	if err != nil {
		log.Println("Ошибка при удалении кнопок:", err)
	}

	// Теперь удаляем само сообщение
	_, err = b.DeleteMessage(chatID, messageID, nil)
	if err != nil {
		log.Println("Ошибка при удалении сообщения:", err)
	}

	_ = lp.saveMessageToSession(userID, sessionID, promptID)

	// Устанавливаем процесс in_session у текущего юзера
	err = lp.updateUserProcess(userID, "in_session")
	if err != nil {
		log.Println("Ошибка при установке процесса in_session:", err)
	} else {
		log.Println("Процесс успешно обновлен на in_session для userID:", userID)
	}

	// Сбрасываем waiting_for_prompt в false**
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")
	_, err = collection.UpdateOne(
		context.TODO(),
		bson.M{"session_id": sessionID},
		bson.M{"$set": bson.M{"waiting_for_prompt": false}},
	)
	if err != nil {
		log.Println("Ошибка при обновлении waiting_for_prompt:", err)
		return err
	}

	log.Printf("getPromptFromDB вызван с promptID: %s", promptID)

	// получаем промт из базы
	promptText, err := lp.getPromptFromDB(promptID)
	if err != nil {
		log.Println("Ошибка при получении промта:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}
	prompt := []openai.ChatCompletionMessage{
		{
			Role:    "user",
			Content: promptText,
		},
	}

	log.Println("== История перед отправкой в GPT ==")
	sessionHistory, _ := lp.getAllMessages(userID)
	for _, msg := range sessionHistory {
		log.Printf("%s: %s\n", msg.Role, msg.Content)
	}
	log.Println("== Конец истории ==")

	// отправляем промт к GPT
	response, err := lp.sendToChatGPT(userID, prompt)
	if err != nil {
		log.Println("Ошибка при запросе к GPT:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	// отправляем ответ юзеру
	_, err = b.SendMessage(userID, response, nil)
	if err != nil {
		log.Println("Ошибка при отправке сообщения:", err)
		return err
	}

	//cохраняем сообщение в сессии
	err = lp.saveMessageToSession(userID, sessionID, response)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения в сессии:", err)
		return err
	}

	return nil
}

func (lp *LongPoll) handleUserMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id
	userText := ctx.EffectiveMessage.Text

	if userText == "" {
		return nil
	}

	// Проверяем процесс пользователя
	userProcess, err := lp.getUserProcess(userID)
	if err != nil {
		log.Println("Ошибка при получении процесса пользователя:", err)
		return err
	}
	sessionID, _, err := lp.getOrCreateSession(userID)
	if err != nil {
		log.Println("Ошибка при получении sessionID:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	// Если процесс "support", сохраняем сообщение в коллекцию поддержки
	if userProcess == "support" {
		err := lp.saveSupportMessage(userID, userText)
		if err != nil {
			log.Println("Ошибка при сохранении сообщения в поддержку:", err)
			return err
		}

		_, _ = b.SendMessage(userID, "Ваше сообщение отправлено в поддержку.", nil)

		// Обновляем процесс с support на none  у текущего юзера
		err = lp.updateUserProcess(userID, "none")
		if err != nil {
			log.Println("Ошибка при обновлении процесса юзера с support на none:", err)
		}

		err = lp.closeSession(sessionID)
		if err != nil {
			log.Println("Ошибка при закрытии сессии:", err)
			return err
		}

		return nil
	}

	// Если процесс "feedback", сохраняем сообщение в коллекцию feedback
	if userProcess == "feedback" {
		err := lp.saveFeedbackMessage(userID, userText)
		if err != nil {
			log.Println("Ошибка при сохранении сообщения в feedback:", err)
			return err
		}

		_, _ = b.SendMessage(userID, "Спасибо, ваш отзыв очень важен.", nil)

		// Обновляем процесс с feedback на none  у текущего юзера
		err = lp.updateUserProcess(userID, "none")
		if err != nil {
			log.Println("Ошибка при обновлении процесса юзера с support на none:", err)
		}

		err = lp.closeSession(sessionID) //TODO ДОБАВИЛ ЭТО СЮДА ПРОВЕРЯЮ ЗАКРОЕТСЯ ЛИ СЕССИЯ ПОСЛЕ ОТПРАВКИ СМС ФИДА
		if err != nil {
			log.Println("Ошибка при закрытии сессии:", err)
			return err
		}

		return nil
	}

	// Проверяем, есть ли у пользователя закрытая сессия
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")
	var lastSession bson.M
	err = collection.FindOne(context.TODO(), bson.M{
		"user_id":   userID,
		"is_closed": false, //
	}, options.FindOne().SetSort(bson.D{{"created_at", -1}})).Decode(&lastSession)

	if err != nil && err != mongo.ErrNoDocuments {
		log.Println("Ошибка при проверке последней сессии:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	// === 1. Блокируем сообщения, если юзер не выбрал промт ===
	if lastSession != nil {
		if waiting, ok := lastSession["waiting_for_prompt"].(bool); ok && waiting {
			_, _ = b.SendMessage(userID, "Сначала выберите тему, затем можете писать сообщения.", nil)
			return nil
		}
	}
	// === 2. Блокируем сообщения, если последняя сессия закрыта ===
	if lastSession != nil {
		if closed, ok := lastSession["is_closed"].(bool); ok && closed {
			_, _ = b.SendMessage(userID, "Последняя сессия закрыта. Вы можете начать новый диалог.", nil)
			return nil
		}
	}

	sessionID, existingSession, err := lp.getOrCreateSession(userID)
	if err != nil {
		log.Println("Ошибка при получении sessionID:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	if existingSession["waiting_for_prompt"].(bool) {
		_, _ = b.SendMessage(userID, "Сначала выберите тему.", nil)
		return nil
	}

	// Если сессия закрыта, не даем писать
	if existingSession["is_closed"].(bool) {
		_, _ = b.SendMessage(userID, "Сессия закрыта. Вы можете начать новый диалог.", nil)
		return nil
	}

	// Получаем счетчик сообщений пользователя
	userMessageCount, ok := existingSession["user_message_count"].(int)
	if !ok {
		if val, ok := existingSession["user_message_count"].(int32); ok {
			userMessageCount = int(val)
		} else if val, ok := existingSession["user_message_count"].(int64); ok {
			userMessageCount = int(val)
		} else {
			log.Println("Ошибка: user_message_count имеет неожиданный тип", reflect.TypeOf(existingSession["user_message_count"]))
			userMessageCount = 0 // Или обработать по-другому
		}
	}

	if userMessageCount >= 3 {
		// Закрываем сессию
		err = lp.closeSession(existingSession["session_id"].(string))
		if err != nil {
			log.Println("Ошибка при закрытии сессии:", err)
			return err
		}
		// Обновляем процесс юзера с in_session на none  у текущего юзера
		err = lp.updateUserProcess(userID, "none")
		if err != nil {
			log.Println("Ошибка при обновлении процесса с in_session на none:", err)
		}

		_, _ = b.SendMessage(userID, "Лимит сообщений исчерпан. Вы можете начать новый диалог.", nil)

		return lp.handleEndOfSession(b, ctx)

	}

	// Сохраняем сообщение пользователя в коллекцию dialogues
	err = lp.saveUserMessage(userID, sessionID, userText)
	if err != nil {
		log.Println("Ошибка при сохранении текста пользователя в коллекцию dialogues:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}

	err = lp.incrementUserMessageCount(existingSession["session_id"].(string))
	if err != nil {
		log.Println("Ошибка при обновлении счетчика сообщений:", err)
		return err
	}

	messages, err := lp.getAllMessages(userID)
	if err != nil {
		log.Println("Ошибка при получении истории сообщений:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}
	log.Println("История сообщений перед отправкой в GPT:", messages)

	log.Println("Отправляем в GPT:", messages)

	//отправляем сообщения чату гпт
	response, err := lp.sendToChatGPT(userID, messages)
	if err != nil {
		log.Println("Ошибка при запросе к GPT:", err)
		_, _ = b.SendMessage(userID, "Ошибка сервера, попробуйте позже.", nil)
		return err
	}
	_, err = b.SendMessage(userID, response, nil)
	if err != nil {
		log.Println("Ошибка при отправке сообщения:", err)
		return err
	}
	// Сохраняем ответ GPT в диалог
	err = lp.saveMessageToSession(userID, sessionID, response)
	if err != nil {
		log.Println("Ошибка при сохранении ответа GPT:", err)
		return err
	}

	return nil
}

func (lp *LongPoll) handleBuySubscription(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id

	// Получаем пользователя
	user, err := lp.getUserByID(userID)
	if err != nil {
		log.Println("Ошибка при получении пользователя:", err)
		return err
	}

	if user == nil {
		_, _ = b.SendMessage(userID, "Ошибка: пользователь не найден.", nil)
		return nil
	}
	// Читаем текущую подписку
	currentPlan, _ := user["plan"].(string)
	subscriptionEnd, _ := user["subscription_end"].(primitive.DateTime)
	sessionLeft, _ := user["session_left"].(int)

	// Проверяем, истекла ли подписка
	isExpired := subscriptionEnd.Time().Before(time.Now())

	// Проверяем, может ли он обновить подписку
	var messageToUser string
	var keyboard [][]gotgbot.InlineKeyboardButton

	switch currentPlan {
	case "premium":
		messageToUser = fmt.Sprintf("У вас уже активен тариф 'Premium'. Он истекает: %s.\n"+
			"Вы сможете продлить подписку после окончания.",
			subscriptionEnd.Time().Format("02.01.2006"))
	case "standard":
		messageToUser = "У вас активен тариф 'Standard'. Вы можете перейти на 'Premium'."
		keyboard = [][]gotgbot.InlineKeyboardButton{
			{gotgbot.InlineKeyboardButton{Text: "Купить Premium", CallbackData: "sub_premium"}},
		}
	case "basic":
		messageToUser = "У вас активен тариф 'Basic'. Вы можете перейти на 'Standard' или 'Premium'."
		keyboard = [][]gotgbot.InlineKeyboardButton{
			{gotgbot.InlineKeyboardButton{Text: "Купить Standard", CallbackData: "sub_standard"}},
			{gotgbot.InlineKeyboardButton{Text: "Купить Premium", CallbackData: "sub_premium"}},
		}
	case "trial":
		if isExpired || sessionLeft == 0 {
			log.Println("⚠Пробный период истёк. Показываем выбор тарифов.")
			messageToUser = "Ваш пробный период истёк. Выберите тарифный план:"
			keyboard = [][]gotgbot.InlineKeyboardButton{
				{gotgbot.InlineKeyboardButton{Text: "Basic (30 дней, 30 сессий)", CallbackData: "sub_basic"}},
				{gotgbot.InlineKeyboardButton{Text: "Standard (60 дней, 60 сессий)", CallbackData: "sub_standard"}},
				{gotgbot.InlineKeyboardButton{Text: "Premium (90 дней, безлимит)", CallbackData: "sub_premium"}},
			}
		} else {
			messageToUser = fmt.Sprintf("Вы используете пробный тариф. Он истекает: %s.",
				subscriptionEnd.Time().Format("02.01.2006"))
		}
	default:
		log.Println("У пользователя нет активной подписки. Показываем все тарифы.")
		messageToUser = "У вас нет активной подписки. Выберите тарифный план:"
		keyboard = [][]gotgbot.InlineKeyboardButton{
			{gotgbot.InlineKeyboardButton{Text: "Basic (30 дней, 30 сессий)", CallbackData: "sub_basic"}},
			{gotgbot.InlineKeyboardButton{Text: "Standard (60 дней, 60 сессий)", CallbackData: "sub_standard"}},
			{gotgbot.InlineKeyboardButton{Text: "Premium (90 дней, безлимит)", CallbackData: "sub_premium"}},
		}

	}
	//Отправляем сообщение с кнопками
	opts := &gotgbot.SendMessageOpts{}
	if len(keyboard) > 0 {
		opts.ReplyMarkup = gotgbot.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}
	_, err = b.SendMessage(userID, messageToUser, opts)
	if err != nil {
		log.Println("Ошибка при отправке сообщения о подписке:", err)
	}

	return nil
}

func (lp *LongPoll) handleSubscriptionCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveUser.Id
	plan := ctx.CallbackQuery.Data // sub_basic, sub_standard, sub_premium
	chatID := ctx.EffectiveChat.Id
	messageID := ctx.EffectiveMessage.MessageId

	// Определяем тарифный план
	var planName string
	switch plan {
	case "sub_basic":
		planName = "basic"
	case "sub_standard":
		planName = "standard"
	case "sub_premium":
		planName = "premium"
	default:
		_, _ = b.SendMessage(userID, "Некорректный выбор подписки.", nil)
		return nil
	}

	// Покупаем подписку
	err := lp.buySubscription(userID, planName)
	if err != nil {
		log.Println("Ошибка при покупке подписки:", err)
		_, _ = b.SendMessage(userID, "Ошибка при оформлении подписки.", nil)
		return err
	}

	// Удаляем кнопки выбора тарифа
	_, _, err = b.EditMessageReplyMarkup(&gotgbot.EditMessageReplyMarkupOpts{
		ChatId:      chatID,
		MessageId:   messageID,
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{}, // Удаляем кнопки
	})
	if err != nil {
		log.Println("Ошибка при удалении кнопок:", err)
	}

	// Удаляем сообщение
	_, err = b.DeleteMessage(chatID, messageID, nil)
	if err != nil {
		log.Println("Ошибка при удалении сообщения:", err)
	}

	// Отвечаем пользователю
	_, _ = b.SendMessage(userID, "Вы успешно оформили подписку: "+planName, nil)

	return nil
}

func (lp *LongPoll) handleEndOfSession(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveUser.Id

	log.Printf("[handleEndOfSession] Обрабатываем команду /close от userID=%d", userID)

	sessionID, _, err := lp.getOrCreateSession(userID)
	if err != nil {
		log.Println("[handleEndOfSession] Ошибка при получении или создании сессии:", err)
		log.Println("Ошибка при получении или создании сессии:", err)
		_, _ = b.SendMessage(userID, "У вас нет активной сессии.", nil)
		return err
	}

	log.Printf("[handleEndOfSession] Получена сессия: sessionID=%s", sessionID)

	err = lp.closeSession(sessionID)
	if err != nil {
		log.Println("Ошибка при закрытии сессии:", err)
		log.Println("Ошибка при получении или создании сессии:", err)
		return err
	}
	log.Printf("[handleEndOfSession] Сессия sessionID=%s закрыта", sessionID)

	_, _ = b.SendMessage(userID, "Сессия завершена. Вы можете начать новый диалог.", nil)

	return lp.handlerRequestSessionFeedback(b, ctx)
}

func (lp *LongPoll) handleTechSupportCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id

	// Устанавливаем процесс support у текущего юзера
	err := lp.updateUserProcess(userID, "support")
	if err != nil {
		log.Println("Ошибка при установке процесса support:", err)
	}
	_, err = b.SendMessage(userID, "Отправьте ваше сообщение в поддержку:", nil)
	return err
}

func (lp *LongPoll) handleBotFeedbackCommand(b *gotgbot.Bot, ctx *ext.Context) error {
	userID := ctx.EffectiveMessage.From.Id

	// Устанавливаем процесс feedback у текущего юзера
	err := lp.updateUserProcess(userID, "feedback")
	if err != nil {
		log.Println("Ошибка при установке процесса support:", err)
	}

	_, err = b.SendMessage(userID, "Отправьте обратную связь на бота:", nil)
	return err
}

func (lp *LongPoll) handleText(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveMessage.Chat
	return lp.sendText(chat.Id, TextDefault)
}

func (lp *LongPoll) sendText(chatId int64, text string) error {
	_, err := lp.bot.SendMessage(chatId, text, &gotgbot.SendMessageOpts{})
	return err
}

func (lp *LongPoll) handleCreateApteka(b *gotgbot.Bot, ctx *ext.Context) error {
	chat := ctx.EffectiveMessage.Chat
	createAptekaKeyboard := &gotgbot.ReplyKeyboardMarkup{
		OneTimeKeyboard: true,
		ResizeKeyboard:  true,
		Keyboard: [][]gotgbot.KeyboardButton{
			{
				{Text: "Создать аптеку", WebApp: &gotgbot.WebAppInfo{Url: createAptekaWebAppUrl}},
			},
		},
	}
	_, err := lp.bot.SendMessage(chat.Id, TextCreateApteka,
		&gotgbot.SendMessageOpts{ReplyMarkup: createAptekaKeyboard})
	return err
}
