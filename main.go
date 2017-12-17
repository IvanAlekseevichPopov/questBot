package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/go-telegram-bot-api/telegram-bot-api"
)

//const testChatId int64 = 75808241
const botToken = "347808432:AAFJQQOUDKCHFBaSxAbVCykyIMa-D9dCcE4"

type storyIteration struct {
	Monologue []string
	Question  string
	Answers   []string
	Prompt    string
	GoTo      string
}

type userSession struct {
	Stuff    map[string]string //Shoulder bag
	Position string            //Position in user story
	Lock     bool              //is user locked for runtime
}

var bot *tgbotapi.BotAPI
var story map[string]storyIteration
var userAnswers map[string]string
var sessions = make(map[int64]userSession)

func init() {
	loadStory()
	checkStory()

	initBot()
}

func main() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		log.Panic(err)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		proceedMessage(update.Message.Chat.ID, update.Message.Text)
	}
}

func proceedMessage(chatId int64, messageFromUser string) {

	fmt.Println(messageFromUser)
	sess, err := sessionGet(chatId)

	if !err {
		//Сессия найдена - продолжаем
		//fmt.Println("Сессия найдена")

		if sess.Lock {
			//fmt.Println("Заблокирован ввод пользователя")
		} else {
			lastStorySubject := story[sess.Position]
			//fmt.Println("Последний зарегистрированный кусок сюжета", lastStorySubject)

			currentStorySubject, err := getCurrentPosition(messageFromUser, lastStorySubject)
			if len(err) > 0 {
				msg := tgbotapi.NewMessage(chatId, err)
				bot.Send(msg)

				return
			}

			proceedPrompt(messageFromUser, lastStorySubject, &sess)

			sess = sessionSet(chatId, userSession{ //TODO передача по ссылке, передаем только изменяемый параметр
				Position: userAnswers[messageFromUser],
				Lock:     true,
				Stuff:    sess.Stuff,
			})

			showMonologue(chatId, currentStorySubject.Monologue)

			askQuestion(chatId, currentStorySubject)

			sessionSet(chatId, userSession{
				Position: userAnswers[messageFromUser], //TODO Атата по уродски читаем из мапы 2 раза
				Lock:     false,
				Stuff:    sess.Stuff,
			})

			//fmt.Println(sess.Stuff)
		}
	} else {
		//Сессия не найдена - создаем новую, рисуем главное меню
		//sendStartMenu(chatId)
		sessionStart(chatId)
		redrawLastPosition(chatId)
	}
}

func proceedPrompt(userMessage string, lastStorySubject storyIteration, sess *userSession) {
	if len(lastStorySubject.Prompt) > 0 {
		if nil == sess.Stuff {
			sess.Stuff = make(map[string]string)
		}

		sess.Stuff[lastStorySubject.Prompt] = userMessage
	}
}

func getCurrentPosition(messageFromUser string, lastStorySubject storyIteration) (storyIteration, string) {

	if len(lastStorySubject.Answers) > 0 {
		//Проверяем, если предыдущая итерация закончилась выбором ответа
		newPositionName, ok := userAnswers[messageFromUser]
		if !ok {
			return storyIteration{}, "Я вас не понимаю1."
		}

		currentStorySubject, ok := story[newPositionName]
		if !ok {
			return storyIteration{}, "Я вас не понимаю2."
		}

		isCorrectUserMessage := false
		for _, answer := range lastStorySubject.Answers {
			if answer == messageFromUser {
				isCorrectUserMessage = true
			}
		}

		if !isCorrectUserMessage {
			return storyIteration{}, "Я вас не понимаю3."
		}

		return currentStorySubject, ""

	} else if len(lastStorySubject.Prompt) > 0 && len(lastStorySubject.GoTo) > 0 { //Проверяем, если предыдущая итерация закончилась запросом пользовательского ввода
		//TODO Проверка ввода пользователя на ругательства

		currentStorySubject, ok := story[lastStorySubject.GoTo]
		if !ok {
			return storyIteration{}, "Я вас не понимаю4."
		}

		return currentStorySubject, ""
	} else {
		fmt.Println("Неизвестно, что делать дальше")
		fmt.Println(lastStorySubject)
		fmt.Println(messageFromUser)
		os.Exit(1)
	}

	return storyIteration{}, "Что-то явно пошло не так. Загляни в консоль"
}

func redrawLastPosition(chatId int64) {
	sess, _ := sessionGet(chatId)

	currentStoryObject := story[sess.Position]

	showMonologue(chatId, currentStoryObject.Monologue)

	askQuestion(chatId, currentStoryObject)
}

func sessionStart(chatId int64) {
	sessions[chatId] = userSession{
		Lock:     false,
		Position: "menu",
	}
}

func sessionGet(chatId int64) (userSession, bool) {
	session, err := sessions[chatId]
	return session, !err
	//return sessions[chatId]
}

func sessionSet(chatId int64, session userSession) userSession {
	sessions[chatId] = session
	return session
}

//func sendStartMenu(chatId int64) {
//	markup := tgbotapi.NewReplyKeyboard(
//		tgbotapi.NewKeyboardButtonRow(
//			tgbotapi.KeyboardButton{Text: "Начать игру"},
//			tgbotapi.KeyboardButton{Text: "Перезапуск"},
//		),
//	)
//	markup.OneTimeKeyboard = true
//
//	msg := tgbotapi.NewMessage(chatId, "Главное меню")
//	msg.ReplyMarkup = &markup
//	bot.Send(msg)
//}

func showMonologue(chatId int64, monologCollection []string) {
	for _, monologue := range monologCollection {
		msg := generateTextMessage(chatId, monologue)

		bot.Send(msg)

		//time.Sleep(time.Second * 1)
	}
}

func askQuestion(chatId int64, currentStoryPosition storyIteration) {
	//msg := tgbotapi.NewMessage(chatId, currentStoryPosition.Question)
	msg := generateTextMessage(chatId, currentStoryPosition.Question)

	if len(currentStoryPosition.Answers) > 0 { // Выбор из готового ответа
		var keyBoardButtonGroup []tgbotapi.KeyboardButton

		for _, button := range currentStoryPosition.Answers {
			//fmt.Println(button)
			keyBoardButtonGroup = append(keyBoardButtonGroup, tgbotapi.KeyboardButton{
				Text:            button,
				RequestContact:  false,
				RequestLocation: false,
			})
		}

		markup := tgbotapi.NewReplyKeyboard(keyBoardButtonGroup)
		markup.OneTimeKeyboard = true
		msg.ReplyMarkup = &markup
	} else if len(currentStoryPosition.Prompt) > 0 { // Ожидание ввода от пользователя
		//TODO Ждем ввода имени иль ничего не делаем

	}

	bot.Send(msg)
}

func generateTextMessage(chatId int64, message string) tgbotapi.MessageConfig {
	sess, _ := sessionGet(chatId)
	fmt.Println("Generating.", sess.Stuff)

	for stuffKey, stuffItem := range sess.Stuff {
		message = strings.Replace(message, "["+stuffKey+"]", stuffItem, -1)
		fmt.Println("Сгенерированный текст", message)
	}

	return tgbotapi.NewMessage(chatId, message)
}

func sendImage(chatId int64, imagePath string) {
	msg := tgbotapi.NewPhotoUpload(chatId, imagePath)

	bot.Send(msg)
}

func sendAudio(chatId int64, trackPath string) {
	msg := tgbotapi.NewAudioUpload(chatId, trackPath)

	bot.Send(msg)
}

func inArray(id int64, array map[int64]userSession) bool {
	for key := range array {
		if key == id {
			return true
		}
	}
	return false
}

func loadStory() {
	//bot.Debug = true
	file, err := ioutil.ReadFile("./story.json")
	if err != nil {
		fmt.Printf("File error: %v\n", err)
		os.Exit(1)
	}

	json.Unmarshal(file, &story)

	file, err = ioutil.ReadFile("./userAnswers.json")
	if err != nil {
		fmt.Printf("File error: %v\n", err)
		os.Exit(1)
	}

	json.Unmarshal(file, &userAnswers)

	log.Printf("Story is loaded")
}

func checkStory() {
	//Проверка мапы на корректность
	//TODO - не должно быть в отдной и той же итерации Answers и Prompt
	//TODO - если есть Prompt, должен быть и GoTo
}

func initBot() {
	err := error(nil)
	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
}
