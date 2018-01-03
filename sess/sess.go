package sess

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/boltdb/bolt"
)

const sessionsBucketName = "user_sessions"

type SessionsStruct struct {
	sync.Mutex
	Users map[int64]*UserSession
}

type UserSession struct {
	Stuff       map[string]string //Shoulder bag
	Position    string            //Position in user story
	IsWorking   bool              //is user locked for runtime
	UpdatedAt   time.Time         //For cron flushes, user reminders
	UserId      int64
	NotifyCount int
	sync.Mutex
}

var db *bolt.DB

func (sessions SessionsStruct) Set(chatId int64, session UserSession) {
	sessions.Lock()
	defer sessions.Unlock()

	sessions.Users[chatId] = &session
	////TODO отправить запись в канал
}

func (sessions SessionsStruct) Get(chatId int64, startLink string) *UserSession {
	session, ok := sessions.Users[chatId]

	if !ok {
		//TODO поиск в БД. Выгрузка неактивных сессий в БД
		//sess = dbFind(chatId)
		//fmt.Println("Db find ended")

		//Создаем новую сессию
		session = &UserSession{
			UpdatedAt:   time.Now(),
			IsWorking:   false,
			Position:    startLink,
			UserId:      chatId,
			NotifyCount: 0,
		}

		sessions.Set(chatId, *session)
	}

	return session
}

func (userSession *UserSession) SetPosition(position string) {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.UpdatedAt = time.Now()
	userSession.Position = position

	dbSave(userSession)
}

func (userSession *UserSession) SetUpdatedAt(date time.Time) {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.UpdatedAt = date
	dbSave(userSession)
}

func (userSession *UserSession) IncreaseNotifyCount() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.NotifyCount = userSession.NotifyCount + 1
	dbSave(userSession)
}

func (userSession *UserSession) ResetNotifyCount() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.NotifyCount = 0
	dbSave(userSession)
}

func (userSession *UserSession) AddStuff(item string, value string) {
	userSession.Lock()
	defer userSession.Unlock()

	if nil == userSession.Stuff {
		userSession.Stuff = make(map[string]string)
	}

	userSession.UpdatedAt = time.Now()
	userSession.Stuff[item] = value
	dbSave(userSession)
}

func (userSession *UserSession) ClearStuff() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.Stuff = make(map[string]string)
	userSession.UpdatedAt = time.Now()

	dbSave(userSession)
}

func (userSession *UserSession) SetWorking(flag bool) {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.UpdatedAt = time.Now()
	userSession.IsWorking = flag
	dbSave(userSession)
}

func dbSave(session *UserSession) {
	sessionToSave := *session
	sessionToSave.IsWorking = false //Разблокируем перед сохранением. Иначе подтянутая из базы сессия навсегда заблокирована

	err := db.Update(func(tx *bolt.Tx) error {
		log.Println("Сохраняем сесию", sessionToSave)
		b := tx.Bucket([]byte(sessionsBucketName))

		buf, err := json.Marshal(sessionToSave)
		if nil != err {
			return err
		}

		err = b.Put([]byte(strconv.FormatInt(sessionToSave.UserId, 10)), buf)
		if nil != err {
			return err
		}

		return nil
	})

	if nil != err {
		log.Println("Ошибка сохранения сессии в БД. Остановка", err)
		os.Exit(1)
	}
}

//func dbFind(chatId int64) *UserSession {
//	fmt.Println("^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^dbFIND$$$$$$$$$$$$$$$$$$$$$")
//	var sess = &UserSession{}
//
//	db.View(func(tx *bolt.Tx) error {
//		b := tx.Bucket([]byte(sessionsBucketName))
//
//		v := b.Get([]byte(strconv.FormatInt(chatId, 10)))
//		fmt.Printf("The answer is: %s\n", v)
//
//		//sess := new(UserSession)
//		err := json.Unmarshal(v, &sess)
//
//		fmt.Println("error unmarshal", err)
//		fmt.Println("sess", sess)
//
//		return nil
//	})
//	fmt.Println("3444444444444444444444444444444444")
//
//	return sess
//}

func (sessions *SessionsStruct) LoadSessions(fileName string) {
	//Инициализируем БД
	var err error

	db, err = bolt.Open(fileName, 0600, nil)

	if err != nil {
		log.Fatal(err)
	}

	//Инициализируем корзину
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(sessionsBucketName))
		return err
	})

	if nil != err {
		log.Println("create bucket error:", err)
		os.Exit(1)
	}

	//Заполнение сессий из БД
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(sessionsBucketName))

		c := b.Cursor()

		for k, v := c.First(); k != nil; k, v = c.Next() {
			chatId, err := strconv.ParseInt(string(k), 10, 64) //TODO нормальная конвертация в int64
			if nil != err {
				return err
			}

			session := new(UserSession)
			err = json.Unmarshal(v, &session)

			log.Printf("key=%v value=%v\n", chatId, *session)
			if nil != err {
				return err
			}

			sessions.Users[chatId] = session
		}

		return nil
	})

	if nil != err {
		log.Println("Fill Sessions error:", err)
		os.Exit(1)
	}

	for key, sess := range sessions.Users {
		log.Printf("chat - %d - sess- %+v\n", key, sess)
	}
}

func GetAllSessions(unusedLinks []string, notifications map[int]map[string]string, fn func(session *UserSession, notify map[string]string)) SessionsStruct {
	var sessionsNotify = SessionsStruct{Users: make(map[int64]*UserSession)}

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(sessionsBucketName))
		c := b.Cursor()

	START:
		for k, v := c.First(); k != nil; k, v = c.Next() {
			chatId, err := strconv.ParseInt(string(k), 10, 64) //TODO нормальная конвертация в int64
			if nil != err {
				return err
			}

			session := new(UserSession)

			if err = json.Unmarshal(v, &session); nil != err {
				return err
			}

			log.Printf("key=%v value=%v\n", chatId, *session)

			for _, unusedLink := range unusedLinks {
				if session.Position == unusedLink {
					log.Println("Не отсылаем ничего. Пользователь на нейтральной позиции")
					continue START
				}
			}

			realDiff := time.Since(session.UpdatedAt).Hours()
			log.Println(realDiff)

			notify, ok := notifications[session.NotifyCount]

			if ok {
				log.Println("Есть что отправить. Проверяем прошедшее время")

				needDiff, _ := strconv.ParseFloat(notify["silence_time"], 64)
				if realDiff >= needDiff {
					log.Println("Прошло нужное кол-во времени")

					fn(session, notify)
					sessionsNotify.Set(session.UserId, *session)
				}
			}
		}

		return nil
	})

	if nil != err {
		log.Println("Ошибка при выполнении крон задания", err)
		os.Exit(1)
	}

	return sessionsNotify
}
