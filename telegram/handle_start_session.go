package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/oybek/jethouse/db"
	"github.com/sashabaranov/go-openai"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"strings"
	"time"
)

// пизднануть в базу, забрать промт, дать его гпт, и вернуть ответ чата гпт юзеру
// будет коллекция промтс, пока один промт
// сессию по айдишнику создавать буду, когда она начинается

// обрабатываем нажатие на кнопку в меню выбора промта

// достаем промт из базы
func (lp *LongPoll) getPromptFromDB(promptID string) (string, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("prompt")
	query := bson.M{"_id": promptID}

	var result struct {
		Prompt string `bson:"text"`
	}

	err := collection.FindOne(context.TODO(), query).Decode(&result)
	if err != nil {
		log.Println("Ошибка получения промта:", err)
		return "", err
	}

	return result.Prompt, nil
}

// отправляем промт к GPT
func (lp *LongPoll) sendToChatGPT(userID int64, messages []openai.ChatCompletionMessage) (string, error) {
	client := lp.openaiClient

	// Получаем всю историю сообщений из базы
	historyMessages, err := lp.getAllMessages(userID)
	if err != nil {
		log.Printf("Ошибка при получении истории сообщений: %v", err)
		return "", err
	}
	log.Printf("История сообщений для sessionID=%s: %+v", userID, historyMessages)

	// Добавляем историю сообщений к текущим сообщениям
	messages = append(historyMessages, messages...)

	// Фильтруем сообщения, убирая User: prompt_X
	var filteredMessages []openai.ChatCompletionMessage
	for _, msg := range messages {
		if !strings.HasPrefix(msg.Content, "User: prompt_") {
			filteredMessages = append(filteredMessages, msg)
		}
	}

	// Проверяем, что messages не пуст
	if len(messages) == 0 {
		log.Println("Пустой массив messages, добавляю системное сообщение.")
		messages = []openai.ChatCompletionMessage{{
			Role:    "system",
			Content: "Ты ассистент, который помогает пользователям.",
		}}
	}

	// Отправляем запрос в OpenAI API
	resp, err := client.CreateChatCompletion(context.TODO(), openai.ChatCompletionRequest{
		Model:     openai.GPT4o,
		Messages:  messages,
		MaxTokens: 500,
	})

	if err != nil {
		log.Printf("Ошибка при запросе к GPT: %v", err)
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ от OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

//assistantResponse := "Возвращаю ответ ГПТ: " + prompt

// уникальный чат в чат гпт Арнур - чатайди -1, Я - чат айди 2
// метчу на уровне базы, у юзера чат гпт ай ди

// создаем сессию
func (lp *LongPoll) getOrCreateSession(userID int64) (string, bson.M, error) {
	log.Printf("[getOrCreateSession] Вызвано для userID=%d", userID)
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")

	var existingSession bson.M
	err := collection.FindOne(context.TODO(), bson.M{
		"user_id":   userID,
		"is_closed": false,
	}).Decode(&existingSession)

	if err == nil {
		// Если нашли активную сессию, возвращаем её
		return existingSession["session_id"].(string), existingSession, nil
	} else if err != mongo.ErrNoDocuments {
		log.Println("Ошибка при поиске сессии:", err)
		return "", nil, err
	}

	// Создаем новую сессию
	sessionID := primitive.NewObjectID().Hex()
	session := bson.M{
		"session_id":         sessionID,
		"user_id":            userID,
		"messages":           []bson.M{},
		"created_at":         time.Now(),
		"user_message_count": 0,
		"is_closed":          false,
		"waiting_for_prompt": true,
	}

	_, insertErr := collection.InsertOne(context.TODO(), session)
	if insertErr != nil {
		log.Println("Ошибка при записи в MongoDB:", insertErr)
		return "", nil, insertErr
	}

	log.Println("Создана новая сессия, ID:", sessionID)
	return sessionID, session, nil

}

func (lp *LongPoll) findLastClosedSession(userID int64) (string, bson.M, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")

	var closedSession bson.M
	err := collection.FindOne(
		context.TODO(),
		bson.M{
			"user_id":   userID,
			"is_closed": true,
		},
		options.FindOne().SetSort(bson.D{{"created_at", -1}}),
	).Decode(&closedSession)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			log.Printf("[findLastClosedSession] Нет закрытых сессий для userID=%d", userID)
			return "", nil, nil // Ошибки нет, но сессии тоже нет
		}
		log.Println("Ошибка при поиске закрытой сессии:", err)
		return "", nil, err
	}

	sessionID := closedSession["session_id"].(string)
	return sessionID, closedSession, nil
}

func (lp *LongPoll) closeSession(sessionID string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")

	var session struct {
		UserID int64 `bson:"user_id"`
	}

	err := collection.FindOne(context.TODO(), bson.M{"session_id": sessionID}).Decode(&session)
	if err != nil {
		return err
	}

	_, err = collection.UpdateOne(context.TODO(), bson.M{"session_id": sessionID}, bson.M{
		"$set": bson.M{"is_closed": true},
	})
	if err != nil {
		return err // Ошибка при обновлении сессии
	}

	// Уменьшаем количество активных сессий в коллекции users
	usersCollection := lp.mongoClient.Database(db.Database).Collection("users")
	_, err = usersCollection.UpdateOne(context.TODO(), bson.M{"user_id": session.UserID}, bson.M{
		"$inc": bson.M{"sessions_left": -1}, // Уменьшаем активные сессии на 1
	})

	return err
}

func (lp *LongPoll) incrementUserMessageCount(sessionID string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("sessions")

	_, err := collection.UpdateOne(context.TODO(), bson.M{"session_id": sessionID}, bson.M{
		"$inc": bson.M{"user_message_count": 1},
	})

	return err
}

func (lp *LongPoll) saveUserMessage(userID int64, sessionID string, message string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("dialogues")

	// Создаем сообщение от пользователя
	userMessage := bson.M{
		"session_id": sessionID,
		"user_id":    userID,
		"role":       "user",
		"text":       message,
		"timestamp":  time.Now().Format(time.RFC3339), // Время сообщения
	}

	_, err := collection.InsertOne(context.TODO(), userMessage)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения пользователя:", err)
		return err
	}

	return nil
}

func (lp *LongPoll) saveMessageToSession(userID int64, sessionID string, message string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("dialogues")

	assistantMessage := bson.M{
		"session_id": sessionID,
		"user_id":    userID,
		"role":       "assistant",
		"text":       message,
		"timestamp":  time.Now().Format(time.RFC3339), // Время сообщения
	}

	// Сохраняем сообщение как отдельный документ
	_, err := collection.InsertOne(context.TODO(), assistantMessage)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения от ассистента:", err)
		return err
	}

	return nil
}

func (lp *LongPoll) getAllMessages(userID int64) ([]openai.ChatCompletionMessage, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("dialogues")

	filter := bson.M{"user_id": userID}
	historyMessages := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})

	cursor, err := collection.Find(context.TODO(), filter, historyMessages)
	if err != nil {
		log.Printf("Ошибка запроса к MongoDB: %v", err)
		return nil, err
	}
	//defer cursor.Close(context.TODO())

	var messages []bson.M
	if err = cursor.All(context.TODO(), &messages); err != nil {
		log.Printf("Ошибка чтения сообщений из MongoDB: %v", err)
		return nil, err
	}

	var conversation []openai.ChatCompletionMessage
	for _, msg := range messages {
		role, ok1 := msg["role"].(string)
		text, ok2 := msg["text"].(string)

		if !ok1 || !ok2 {
			log.Printf("Пропускаем сообщение из-за ошибки типов: %+v", msg)
			continue
		}

		conversation = append(conversation, openai.ChatCompletionMessage{
			Role:    role,
			Content: text,
		})
	}

	return conversation, nil
}

func (lp *LongPoll) getUserByID(userID int64) (bson.M, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("users")

	var user bson.M
	err := collection.FindOne(context.TODO(), bson.M{"user_id": userID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // Пользователь не найден
		}
		log.Println("Ошибка при поиске пользователя:", err)
		return nil, err // Ошибка при запросе к базе
	}

	return user, nil
}

func (lp *LongPoll) canStartSession(userID int64) (bool, string, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("users")

	user, err := lp.getUserByID(userID)
	if err != nil {
		return false, "", err
	}

	// Если пользователя нет — создаем нового с триалом
	if user == nil {
		newUser := bson.M{
			"user_id":            userID,
			"plan":               "trial",
			"subscription_start": time.Now(),
			"subscription_end":   time.Now().Add(24 * time.Hour),
			"sessions_left":      2,
			"is_trial_used":      true,
			"process":            "none",
		}
		_, err := collection.InsertOne(context.TODO(), newUser)

		if err != nil {
			return false, "", err
		}
		return true, "Trial activated! You have 2 sessions.", nil
	}

	// Проверяем, не истекла ли подписка
	endDate, ok := user["subscription_end"].(time.Time)

	if ok && time.Now().After(endDate) {
		return false, "Ваша подписка истекла. Приобретите новую", nil
	}

	unlimited, ok := user["unlimited_sessions"].(bool)
	if ok && unlimited {
		return true, "У вас неограниченные сессии.", nil
	}

	// Проверяем, остались ли сессии
	sessionsLeft, ok := user["sessions_left"].(int32)
	if ok && sessionsLeft <= 0 {
		return false, "У вас не осталось сессий. Купите новые", nil
	}

	return true, "Вы можете начать сессиюю.", nil
}

func (lp *LongPoll) buySubscription(userID int64, plan string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("users")

	// Получаем пользователя
	user, err := lp.getUserByID(userID)
	if err != nil {
		return err
	}

	if user == nil {
		return errors.New("user not found")
	}

	unlimitedSessions, _ := user["unlimited_sessions"].(bool)

	// Если дошли сюда, значит покупка возможна — обновляем подписку
	var duration time.Duration
	var sessions int32
	var unlimited bool

	switch plan {
	case "basic":
		duration = 30 * 24 * time.Hour
		sessions = 30
		unlimited = false
	case "standard":
		duration = 60 * 24 * time.Hour
		sessions = 60
		unlimited = false
	case "premium":
		duration = 90 * 24 * time.Hour
		sessions = 0
		unlimited = true
	default:
		return errors.New("invalid subscription plan")
	}

	startDate := time.Now()
	endDate := startDate.Add(duration)

	// Формируем обновление
	update := bson.M{
		"$set": bson.M{
			"plan":               plan,
			"subscription_start": primitive.NewDateTimeFromTime(startDate),
			"subscription_end":   primitive.NewDateTimeFromTime(endDate),
			"unlimited_sessions": unlimited,
		},
	}

	// Логируем обновление
	log.Printf("Обновляем подписку для %d: Plan=%s, Start=%s, End=%s, Unlimited=%t\n",
		userID, plan, startDate.Format("02.01.2006"), endDate.Format("02.01.2006"), unlimited)

	// Если не безлимит
	if !unlimited {
		if unlimitedSessions {
			update["$set"].(bson.M)["sessions_left"] = sessions
		} else {
			update["$inc"] = bson.M{"sessions_left": sessions}
		}
	} else {
		update["$set"].(bson.M)["sessions_left"] = 0
	}

	_, err = collection.UpdateOne(context.TODO(), bson.M{"user_id": userID}, update)
	if err != nil {
		return err
	}

	return nil
}

func (lp *LongPoll) saveSupportMessage(userID int64, message string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("support")

	// Создаем структуру для хранения сообщения
	supportMessage := bson.M{
		"user_id":   userID,
		"message":   message,
		"timestamp": time.Now(),
	}

	// Сохраняем в MongoDB
	_, err := collection.InsertOne(context.TODO(), supportMessage)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения в поддержку:", err)
		return err
	}

	log.Printf("Сообщение от пользователя %d сохранено в поддержку.", userID)
	return nil
}

func (lp *LongPoll) saveFeedbackMessage(userID int64, message string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("feedback")

	// Создаем структуру для хранения сообщения
	feedbackMessage := bson.M{
		"user_id":   userID,
		"message":   message,
		"timestamp": time.Now(),
	}
	// Сохраняем в MongoDB
	_, err := collection.InsertOne(context.TODO(), feedbackMessage)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения в feedback:", err)
		return err
	}

	log.Printf("Сообщение от пользователя %d сохранено в feedback.", userID)
	return nil
}

func (lp *LongPoll) saveFeedbackSessionMessage(userID int64, sessionID, score string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("feedbackKeys")

	// Создаем структуру для хранения сообщения
	sessionRating := bson.M{
		"userID":    userID,
		"sessionID": sessionID,
		"score":     score,
		"timestamp": time.Now(),
	}
	// Сохраняем в MongoDB
	_, err := collection.InsertOne(context.TODO(), sessionRating)
	if err != nil {
		log.Println("Ошибка при сохранении сообщения в feedbackKeys:", err)
		return err
	}

	log.Printf("Сообщение от пользователя %d сохранено в feedbackKeys.", userID)
	return nil
}

func (lp *LongPoll) getUserProcess(userID int64) (string, error) {
	collection := lp.mongoClient.Database(db.Database).Collection("users")
	ctx := context.TODO()

	// Структура для результата
	var user struct {
		Process string `bson:"process"`
	}

	// Ищем пользователя в MongoDB
	err := collection.FindOne(ctx, bson.M{"user_id": userID}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			log.Printf("Пользователь %d не найден в базе", userID)
			return "", nil // Возвращаем пустую строку, если пользователя нет
		}
		log.Println("Ошибка при получении процесса пользователя:", err)
		return "", err
	}

	return user.Process, nil
}

func (lp *LongPoll) updateUserProcess(userID int64, newProcess string) error {
	collection := lp.mongoClient.Database(db.Database).Collection("users")
	ctx := context.TODO()

	update := bson.M{"$set": bson.M{"process": newProcess}}

	_, err := collection.UpdateOne(ctx, bson.M{"user_id": userID}, update)
	if err != nil {
		log.Println("Ошибка при обновлении процесса пользователя:", err)
		return err
	}
	log.Printf("Процесс пользователя %d обновлен на %s", userID, newProcess)

	return nil
}
