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
	Answers   []map[string]string
	Prompt    string
	GoTo      string
	Stuff     string
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
			fmt.Println("Заблокирован ввод пользователя")
			msg := tgbotapi.NewMessage(chatId, "Ввод заблокирован")
			bot.Send(msg)
		} else {
			lastStorySubject := story[sess.Position]

			currentStorySubject, postback, err := getCurrentPosition(messageFromUser, lastStorySubject)
			if len(err) > 0 { //Пишем ошибку и перерисовываем кнопки
				redrawLastPosition(chatId, err, lastStorySubject)

				return
			}

			proceedPrompt(messageFromUser, lastStorySubject, &sess)
			proceedStuff(&postback, &currentStorySubject, &sess)

			sess = sessionSet(chatId, userSession{ //TODO передача по ссылке, передаем только изменяемый параметр
				Position: postback,
				Lock:     true,
				Stuff:    sess.Stuff,
			})

			showMonologue(chatId, currentStorySubject.Monologue)

			isQuestionAsked := askQuestion(chatId, currentStorySubject)
			if !isQuestionAsked && len(currentStorySubject.GoTo) > 0 { //обработка чистого монолога и goto
				redrawLastPosition(chatId, "12", story[currentStorySubject.GoTo])

				sess = sessionSet(chatId, userSession{
					Position: currentStorySubject.GoTo,
					Lock:     false,
					Stuff:    sess.Stuff,
				})
			} else {
				sessionSet(chatId, userSession{
					Position: postback,
					Lock:     false,
					Stuff:    sess.Stuff,
				})
			}
		}
	} else {
		//Сессия не найдена - создаем новую, рисуем главное меню
		//sendStartMenu(chatId)
		sessionStart(chatId)

		startStoryPosition := story["menu"]
		showMonologue(chatId, startStoryPosition.Monologue)
		askQuestion(chatId, startStoryPosition)
	}
}

func proceedStuff(postback *string, currentStoryObject *storyIteration, sess *userSession) {
	if len(currentStoryObject.Stuff) > 0 && len(currentStoryObject.GoTo) > 0 {
		//Берем stuff и сдвигаем вперед сессию
		if nil == sess.Stuff {
			sess.Stuff = make(map[string]string)
		}

		sess.Stuff[currentStoryObject.Stuff] = "true"

		*postback = currentStoryObject.GoTo
		*currentStoryObject = story[currentStoryObject.GoTo]
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

func getCurrentPosition(messageFromUser string, lastStorySubject storyIteration) (storyIteration, string, string) {
	//fmt.Println("Ищем текущую итерацию.Последняя: ", lastStorySubject)

	if len(lastStorySubject.Answers) > 0 {
		//Проверяем, если предыдущая итерация закончилась выбором ответа
		var postback string
		for _, answer := range lastStorySubject.Answers {
			if answer["title"] == messageFromUser {
				postback = answer["postback"]
				break
			}
		}

		if len(postback) > 0 {
			storyItem, ok := story[postback]
			if ok {
				//fmt.Println("Нашел!", storyItem)
				return storyItem, postback, ""
			}
		}

		return storyIteration{}, "", "Я вас не понимаю."

	} else if len(lastStorySubject.Prompt) > 0 && len(lastStorySubject.GoTo) > 0 { //Проверяем, если предыдущая итерация закончилась запросом пользовательского ввода
		//TODO Проверка ввода пользователя на ругательства

		currentStorySubject, ok := story[lastStorySubject.GoTo]
		if !ok {
			return storyIteration{}, "", "Я вас не понимаю4."
		}

		return currentStorySubject, lastStorySubject.GoTo, ""
	}

	log.Println("Неизвестно, что делать дальше")
	fmt.Println(lastStorySubject)
	fmt.Println(messageFromUser)
	return storyIteration{}, "", "Alert! Error! Unknown user reaction"
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

func showMonologue(chatId int64, monologueCollection []string) {
	for _, message := range monologueCollection {
		var msg tgbotapi.Chattable

		if strings.Contains(message, "images") {
			msg = tgbotapi.NewPhotoUpload(chatId, message)
		} else if strings.Contains(message, "sound") {
			msg = tgbotapi.NewPhotoUpload(chatId, message)
		} else {
			msg = generateTextMessage(chatId, message)
		}
		bot.Send(msg)

		//time.Sleep(time.Second * 1)
	}
}

func askQuestion(chatId int64, currentStoryPosition storyIteration) bool {
	if len(currentStoryPosition.Question) > 0 {
		msg := generateTextMessage(chatId, currentStoryPosition.Question)

		if len(currentStoryPosition.Answers) > 0 { // Выбор из готового ответа

			markup := tgbotapi.NewReplyKeyboard()
			for _, button := range currentStoryPosition.Answers {
				row := []tgbotapi.KeyboardButton{{
					Text:            button["title"],
					RequestContact:  false,
					RequestLocation: false,
				}}
				markup.Keyboard = append(markup.Keyboard, row)
			}

			markup.OneTimeKeyboard = true
			msg.ReplyMarkup = &markup

		} else if len(currentStoryPosition.Prompt) > 0 { // Ожидание ввода от пользователя
			//TODO Ждем ввода имени иль ничего не делаем

		}

		bot.Send(msg)
		return true
	}

	return false
	//else if len(currentStoryPosition.GoTo) > 0 { //Только монолог и переход
	//	fmt.Println("Нет вопросов. Есть Goto. Перерисовываем.. ")
	//	redrawLastPosition(chatId, " ", story[currentStoryPosition.GoTo])
	//}
}

func redrawLastPosition(chatId int64, message string, lastStorySubject storyIteration) {
	//TODO половина кода повторяется с askQuestion - вынести общее в другую функцию
	msg := tgbotapi.NewMessage(chatId, message)

	//fmt.Println("Перерисовка изнутри. Answers", lastStorySubject.Answers)
	//fmt.Println("Перерисовка изнутри. GoTo", lastStorySubject.GoTo)
	//if len(lastStorySubject.Answers) == 0 && len(lastStorySubject.GoTo) > 0 {
	//	fmt.Println("Перерисовка. Найден goto")
	//	lastStorySubject = story[lastStorySubject.GoTo]
	//}

	markup := tgbotapi.NewReplyKeyboard()

	for _, button := range lastStorySubject.Answers {
		row := []tgbotapi.KeyboardButton{{
			Text:            button["title"],
			RequestContact:  false,
			RequestLocation: false,
		}}
		markup.Keyboard = append(markup.Keyboard, row)
	}

	markup.OneTimeKeyboard = true
	msg.ReplyMarkup = &markup

	bot.Send(msg)
}

func generateTextMessage(chatId int64, message string) tgbotapi.MessageConfig {
	sess, _ := sessionGet(chatId)

	for stuffKey, stuffItem := range sess.Stuff {
		message = strings.Replace(message, "["+stuffKey+"]", stuffItem, -1)
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
	//TODO - что всем postback соответствуют пункты из истории
}

func initBot() {
	err := error(nil)
	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
}
