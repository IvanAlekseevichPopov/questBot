package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"gopkg.in/yaml.v2"
)

const questStartLink = "first"

type storyIteration struct {
	Monologue  []string
	Question   string
	Answers    []map[string]string
	Prompt     string
	GoTo       string
	Stuff      string //TODO Stuff - массив
	CheckStuff map[string]string
}

type userSession struct {
	Stuff    map[string]string //Shoulder bag
	Position string            //Position in user story
	Lock     bool              //is user locked for runtime
}

type appConfig struct {
	BotToken string `yaml:"bot_token"`
	Env      string `yaml:"env"`
}

var bot *tgbotapi.BotAPI
var story map[string]storyIteration
var userAnswers map[string]string
var sessions = make(map[int64]userSession)
var config appConfig

func init() {
	loadConfig()

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
	if userWantRestart(messageFromUser) {
		err = true
	}

	if !err {

		if sess.Lock {
			fmt.Println("Заблокирован ввод пользователя")
			msg := tgbotapi.NewMessage(chatId, "Ввод заблокирован")
			bot.Send(msg)
		} else {
			fmt.Println(sess)
			lastStorySubject := story[sess.Position]

			currentStorySubject, postback, err := getCurrentPosition(messageFromUser, lastStorySubject)
			if len(err) > 0 { //Пишем ошибку и перерисовываем кнопки
				redrawLastPosition(chatId, err, lastStorySubject)

				return
			}

			//typeOfBlock := calcTypeOfBlock(messageFromUser, lastStorySubject, currentStorySubject)
			//Типы блоков: Запрос ввода, Выбор ответа, Взять Stuff, Проверить Stuff
			//TODO Тип блока указывать в story.json
			//TODO и ему сопоставить необходимые поля

			proceedPrompt(messageFromUser, lastStorySubject, &sess)
			proceedStuff(&postback, &currentStorySubject, &sess)

		link:
			sess = sessionSet(chatId, userSession{ //TODO передача по ссылке, передаем только изменяемый параметр
				Position: postback,
				Lock:     true,
				Stuff:    sess.Stuff,
			})

			showMonologue(chatId, currentStorySubject.Monologue)

			isQuestionAsked := askQuestion(chatId, currentStorySubject)
			fmt.Println("isQuestionAsked - ", isQuestionAsked)

			if !isQuestionAsked && len(currentStorySubject.GoTo) > 0 && len(currentStorySubject.CheckStuff) == 0 { //обработка чистого монолога и goto
				redrawLastPosition(chatId, " ", story[currentStorySubject.GoTo]) //TODO если сообщение будет пустым - все сломается. Нельзя использовать redrawLastPos. Нужно написать еще один метод

				postback = currentStorySubject.GoTo
				currentStorySubject = story[postback]
				sess = sessionSet(chatId, userSession{
					Position: postback,
					Lock:     false,
					Stuff:    sess.Stuff,
				})

				goto link
			} else if !isQuestionAsked && len(currentStorySubject.CheckStuff) > 0 {
				fmt.Println("Обработка проверки снаряжения")
				userStuff := sess.Stuff
				fmt.Println(userStuff)
				for item, failGoTo := range currentStorySubject.CheckStuff {
					_, stuffExist := userStuff[item]
					if !stuffExist {
						postback = failGoTo
						currentStorySubject = story[postback]
						fmt.Println("fail card goto")

						goto link //TODO выпилить это Г.
					}
				}

				postback = currentStorySubject.GoTo
				currentStorySubject = story[postback]
				fmt.Println("success card goto")

				goto link //TODO выпилить это Г.

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
		sessionStart(chatId)

		startStoryPosition := story[questStartLink]
		showMonologue(chatId, startStoryPosition.Monologue)
		askQuestion(chatId, startStoryPosition)
	}
}

func userWantRestart(message string) bool {
	return message == "/start" || message == "start" || message == "/logout" || message == "logout" || message == "/stop"
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

	} else if len(lastStorySubject.Prompt) > 0 && len(lastStorySubject.GoTo) > 0 {
		//Проверяем, если предыдущая итерация закончилась запросом пользовательского ввода
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
		Position: questStartLink,
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

func showMonologue(chatId int64, monologueCollection []string) {
	for _, message := range monologueCollection {
		var msg tgbotapi.Chattable

		if strings.Contains(message, "images") {
			msg = tgbotapi.NewPhotoUpload(chatId, message)
		} else if strings.Contains(message, "sound") {
			msg = tgbotapi.NewAudioUpload(chatId, message)
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
}

func redrawLastPosition(chatId int64, message string, lastStorySubject storyIteration) {
	//TODO половина кода повторяется с askQuestion - вынести общее в другую функцию
	msg := generateTextMessage(chatId, message)

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
	bot, err = tgbotapi.NewBotAPI(config.BotToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
}

func loadConfig() {
	file, err := ioutil.ReadFile("config.yml")
	if err != nil {
		log.Printf("Error opening config.yml: #%v ", err)
		os.Exit(1)
	}

	err = yaml.Unmarshal(file, &config)
	if err != nil {
		log.Fatalf("Error reading config.yml: %v", err)
		os.Exit(1)
	}

	fmt.Printf("%+v\n", config)
}
