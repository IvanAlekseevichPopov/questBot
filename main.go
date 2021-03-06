package main

import (
	"encoding/json"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/joho/godotenv"
	"github.com/robfig/cron"
	"golang.org/x/net/proxy"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"questBot/sess"
	"strings"
	"time"
)

const questStartLink = "first"
const questMeetingLink = "meeting"
const questFinishLink = "exit"

const notifySpitSymbol = "|"

const blockTypeUserInput = 1    //Блок ожидания пользовательского ввода
const blockTypeAnswerChoice = 2 //Блок выбора ответа
const blockTypePutStuff = 3     //Блок пополнения снаряжения
const blockTypeCheckStuff = 4   //Блок проверки необходимого сняряжения
const blockTypeShowMessage = 5  //Блок показа сообщения и переход по GoTo

type storyIteration struct {
	Monologue  []string
	Question   string
	Answers    []map[string]string
	Prompt     string
	GoTo       string
	Stuff      string //TODO Stuff - массив
	CheckStuff map[string]string
}

type appConfig struct {
	Proxy         string
	BotToken      string
	Cron          string
	Notifications map[int]map[string]string `yaml:"user_notifications"`
	Delay         time.Duration
}

var bot *tgbotapi.BotAPI
var story map[string]storyIteration
var config appConfig
var sessions = sess.SessionsStruct{Users: make(map[int64]*sess.UserSession)}

func main() {
	loadConfig("config.yml")                     //TODO in execution parameter
	sessions.LoadSessions("content/sessions.db") //TODO in execution parameter

	loadStory("content/story.json")
	checkStory()

	initBot()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		log.Panic(err)
	}

	enableUserNotify(config.Cron)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		go proceedMessage(update.Message.Chat.ID, update.Message.Text)
	}
}

func proceedMessage(chatId int64, messageFromUser string) {
	log.Println("Сообщение от пользователя:", chatId, messageFromUser)
	session := sessions.Get(chatId, questStartLink)

	if userWantRestart(messageFromUser) {
		session.SetWorking(true)
		session.SetPosition(questStartLink)
		session.ResetNotifyCount()
		session.ClearStuff()

		startStoryPosition := story[questStartLink]
		showMonologue(chatId, startStoryPosition.Monologue)
		askQuestion(chatId, startStoryPosition)

		session.SetWorking(false)
	} else {
		if session.IsWorking {
			log.Println("Заблокирован ввод пользователя")
			return
		}

		lastStorySubject := story[session.Position]
		currentStorySubject, postback, err := getCurrentPosition(messageFromUser, lastStorySubject)

		if len(err) > 0 {
			redrawLastPosition(chatId, err, lastStorySubject)
			return
		}

		session.SetWorking(true) //Заблокировали ввод пользователя
		if questStartLink == postback {
			log.Print("Очищаем Stuff")
			session.ResetNotifyCount()
			session.ClearStuff()
		}

		//Количество переходов по истории без участия пользователя
		for i := 0; i < 3; i++ {
			proceedPrompt(messageFromUser, lastStorySubject, session)

			typeOfBlock := getTypeOfBlock(currentStorySubject)
			switch typeOfBlock {
			case blockTypeUserInput:
				showMonologue(chatId, currentStorySubject.Monologue)
				log.Println("Ожидание пользовательского ввода")
				askQuestion(chatId, currentStorySubject)

				session.SetPosition(postback)
				session.SetWorking(false)
				return

			case blockTypeAnswerChoice:
				log.Println("Выбор ответа")
				showMonologue(chatId, currentStorySubject.Monologue)
				askQuestion(chatId, currentStorySubject)

				session.SetPosition(postback)
				session.SetWorking(false)
				return

			case blockTypePutStuff:
				log.Println("Берем вещь и идем дальше")
				showMonologue(chatId, currentStorySubject.Monologue)
				proceedPutStuff(&postback, &currentStorySubject, session)

				session.SetPosition(postback)
				continue

			case blockTypeCheckStuff:
				log.Println("Есть ли нужное барахло")
				showMonologue(chatId, currentStorySubject.Monologue)
				proceedCheckStuff(&postback, &currentStorySubject, session)
				continue

			case blockTypeShowMessage:
				log.Println("Зачитал и перешел на вопрос. Переносит question в следующую итерацию")

				session.SetPosition(postback)

				postback = currentStorySubject.GoTo
				lastStorySubject = currentStorySubject
				currentStorySubject = story[postback]

				mergeStoryBlocks(&currentStorySubject, &lastStorySubject)
				continue
			}
		}
	}
}

func proceedCheckStuff(postback *string, currentStoryBlock *storyIteration, session *sess.UserSession) {
	userStuff := session.Stuff

	for item, failGoTo := range currentStoryBlock.CheckStuff {
		_, stuffExist := userStuff[item]
		if !stuffExist {
			*postback = failGoTo
			*currentStoryBlock = story[*postback]
			return
		}
	}

	session.SetPosition(*postback)
	*postback = currentStoryBlock.GoTo
	*currentStoryBlock = story[*postback]
}

func getTypeOfBlock(currentStoryBlock storyIteration) int {
	if len(currentStoryBlock.GoTo) == 0 {
		if len(currentStoryBlock.Answers) > 0 && len(currentStoryBlock.Question) > 0 { //Выбор готового решения
			return blockTypeAnswerChoice
		}
	} else {
		if len(currentStoryBlock.Prompt) > 0 { // Ожидание ввода от пользователя
			return blockTypeUserInput
		} else if len(currentStoryBlock.Stuff) > 0 { //Ложим что-то в заплечный мешок
			return blockTypePutStuff
		} else if len(currentStoryBlock.CheckStuff) > 0 { //Проверка сняряги
			return blockTypeCheckStuff
		} else if len(currentStoryBlock.Monologue) > 0 { // Зачитывем монолог и переходим
			return blockTypeShowMessage
		}
	}

	log.Printf("Блок с неизвестным назначением. %+v\n", currentStoryBlock)
	os.Exit(1)
	return 1
}

func userWantRestart(message string) bool {
	return message == "/start" || message == "start" || message == "/logout" || message == "logout" || message == "/stop"
}

func proceedPutStuff(postback *string, currentStoryObject *storyIteration, session *sess.UserSession) {
	if len(currentStoryObject.Stuff) > 0 && len(currentStoryObject.GoTo) > 0 {
		//Берем stuff и сдвигаем вперед сессию
		if nil == session.Stuff {
			session.Stuff = make(map[string]string)
		}

		session.AddStuff(currentStoryObject.Stuff, "1")

		*postback = currentStoryObject.GoTo
		*currentStoryObject = story[currentStoryObject.GoTo]
	}
}

func proceedPrompt(userMessage string, lastStorySubject storyIteration, session *sess.UserSession) {
	if len(lastStorySubject.Prompt) > 0 {
		if nil == session.Stuff {
			session.Stuff = make(map[string]string)
		}

		session.AddStuff(lastStorySubject.Prompt, userMessage)
		log.Println("Записали в stuff")
	}
}

func getCurrentPosition(messageFromUser string, lastStorySubject storyIteration) (storyIteration, string, string) {
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
				return storyItem, postback, ""
			}
		}

		return storyIteration{}, "", "Я тебя не понимаю."

	} else if len(lastStorySubject.Prompt) > 0 && len(lastStorySubject.GoTo) > 0 {
		//Проверяем, если предыдущая итерация закончилась запросом пользовательского ввода
		//TODO Проверка ввода пользователя на ругательства

		currentStorySubject, ok := story[lastStorySubject.GoTo]
		if !ok {
			return storyIteration{}, "", "Я тебя не понимаю.."
		}

		return currentStorySubject, lastStorySubject.GoTo, ""
	} else {
		log.Println("Получение позиции...Неизвестно, что делать дальше", lastStorySubject, messageFromUser)
		os.Exit(1)
	}

	return storyIteration{}, "", "Alert! Error! Unknown user reaction"
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

		messageDelay()
		bot.Send(msg)
	}
	messageDelay()
}

func askQuestion(chatId int64, currentStoryPosition storyIteration) {
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
	}

	bot.Send(msg)
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
	session := sessions.Get(chatId, questStartLink)

	for stuffKey, stuffItem := range session.Stuff {
		message = strings.Replace(message, "["+stuffKey+"]", stuffItem, -1)
	}

	return tgbotapi.NewMessage(chatId, message)
}

func loadStory(fileName string) {
	file, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Printf("Story loading error error: %v\n", err)
		os.Exit(1)
	}

	json.Unmarshal(file, &story)
	log.Println("Story is loaded")
}

func checkStory() {
	//Проверка мапы на корректность
	//TODO - не должно быть в отдной и той же итерации Answers и Prompt
	//TODO - если есть Prompt, должен быть и GoTo
	//TODO - что всем postback соответствуют пункты из истории
}

func initBot() {
	err := error(nil)

	client := &http.Client{}
	if len(config.Proxy) > 0 {
		tgProxyURL, err := url.Parse(config.Proxy)
		if err != nil {
			log.Panic("Failed to parse proxy URL", err)
			os.Exit(1)
		}

		tgDialer, err := proxy.FromURL(tgProxyURL, proxy.Direct)
		if err != nil {
			log.Printf("Failed to obtain proxy dialer: %s\n", err)
			os.Exit(1)
		}
		tgTransport := &http.Transport{
			Dial: tgDialer.Dial,
		}
		client.Transport = tgTransport
	}

	bot, err = tgbotapi.NewBotAPIWithClient(config.BotToken, client)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
}

func loadConfig(fileName string) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
		os.Exit(1)
	}

	config.Cron = os.Getenv("CRON")
	config.Proxy = os.Getenv("PROXY")
	config.BotToken = os.Getenv("BOT_TOKEN")
	config.Delay, _ = time.ParseDuration(os.Getenv("DELAY"))

	if len(config.BotToken) < 10 {
		log.Println("Config error: invalid bot token", config.BotToken)
		os.Exit(1)
	}

	if len(config.Cron) < 4 {
		log.Println("Config error: invalid cron", config.Cron)
		os.Exit(1)
	}

	if 0 == config.Delay.Nanoseconds() || config.Delay.Seconds() > 5 {
		log.Println("Config error: invalid delay", config.Delay.String())
		os.Exit(1)
	}

	//TODO убрать yaml конфиг совсем
	file, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Printf("Error opening %s: #%v ", fileName, err)
		os.Exit(1)
	}

	err = yaml.Unmarshal(file, &config)
	if err != nil {
		log.Fatalf("Error reading %s: %v", fileName, err)
		os.Exit(1)
	}

	for taskId, task := range config.Notifications {
		delay, ok := task["silence_time"]
		if !ok {
			log.Println("Config error: not found silence_time", taskId)
			os.Exit(1)
		}
		if len(delay) == 0 {
			log.Println("Config error: invalid silence_time", taskId)
			os.Exit(1)
		}

		message, ok := task["message"]
		if !ok {
			log.Println("Config error: not found message", taskId)
			os.Exit(1)
		}
		if len(message) < 3 {
			log.Println("Config error: invalid message", taskId)
			os.Exit(1)
		}
	}
}

func mergeStoryBlocks(currentStorySubject *storyIteration, lastStorySubject *storyIteration) {
	if len(currentStorySubject.Question) == 0 {
		if len(currentStorySubject.Monologue) == 0 {
			//Если нет монолога и вопроса - последний из монолога переносим в вопрос.
			//Остальное - в монолог
			monologue := lastStorySubject.Monologue
			question := ""

			if len(lastStorySubject.Monologue) > 1 {
				question = monologue[len(monologue)-1]
				monologue = monologue[:len(monologue)-1]
			} else if len(lastStorySubject.Monologue) == 1 {
				question = monologue[len(monologue)-1]
				monologue = []string{}
			} else {
				log.Println("Недостижимое условие!!!")
				os.Exit(1)
			}

			currentStorySubject.Question = question
			currentStorySubject.Monologue = monologue
		} else {
			//Если нет вопроса и есть монолог - мержим монологи
			currentStorySubject.Monologue = append(lastStorySubject.Monologue, currentStorySubject.Monologue...)
		}
	} else {
		if len(currentStorySubject.Monologue) == 0 {
			//Если есть вопрос и нет монолога - переносим монолог
			currentStorySubject.Monologue = lastStorySubject.Monologue
		} else {
			//Если есть вопрос и есть монолог - мержим монологи. Вопрос не трогаем
			currentStorySubject.Monologue = append(lastStorySubject.Monologue, currentStorySubject.Monologue...)
		}
	}
}

func notifyUser(session *sess.UserSession, notify map[string]string) {
	currentPosition := story[session.Position]
	currentPosition.Monologue = []string{}
	notifyMessages := strings.Split(notify["message"], notifySpitSymbol)
	if len(notifyMessages) > 1 {
		last := len(notifyMessages) - 1
		currentPosition.Question = notifyMessages[last]
		notifyMessages = notifyMessages[:last]
		showMonologue(session.UserId, notifyMessages)
	} else {
		currentPosition.Question = notifyMessages[0]
	}

	askQuestion(session.UserId, currentPosition)
}

func messageDelay() {
	time.Sleep(config.Delay)
}

func enableUserNotify(crontime string) {
	//Напоминания о забытом боте для пользователя
	log.Println("Поставили крон", crontime)
	c := cron.New()

	c.AddFunc(crontime, func() {
		log.Println("Запустили крон")
		//log.Println("Количество сессий - ", len(sessions.Users))
		//var sessionsToUpdate []int64
		//Ищем в БД подходяще чаты для напоминалок пользователей

		ignoreLinks := []string{questStartLink, questFinishLink, questMeetingLink}
		sessionsNotify := sess.GetAllSessions(ignoreLinks, config.Notifications, notifyUser)

		for _, session := range sessionsNotify.Users {
			//notifyUser(session, notify) //TODO Как узнать нотификацию???
			session.IncreaseNotifyCount()
			sessions.Set(session.UserId, *session)
		}
	})

	c.Start()
}
