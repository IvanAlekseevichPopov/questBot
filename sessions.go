package main

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/boltdb/bolt"
)

type Sessions struct {
	sync.Mutex
	users map[int64]*UserSession
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

var sessions = Sessions{users: make(map[int64]*UserSession)}
var db *bolt.DB

func (sessions Sessions) set(chatId int64, session UserSession) {
	sessions.Lock()
	defer sessions.Unlock()

	sessions.users[chatId] = &session
	////TODO отправить запись в канал
}

func (sessions Sessions) get(chatId int64) *UserSession {
	session, ok := sessions.users[chatId]

	if !ok {
		//TODO поиск в БД. Выгрузка неактивных сессий в БД
		//session = dbFind(chatId)
		//fmt.Println("Db find ended")

		//Создаем новую сессию
		session = &UserSession{
			UpdatedAt:   time.Now(),
			IsWorking:   false,
			Position:    questStartLink,
			UserId:      chatId,
			NotifyCount: 0,
		}

		sessions.set(chatId, *session)
	}

	return session
}

func (userSession *UserSession) setPosition(position string) {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.UpdatedAt = time.Now()
	userSession.Position = position

	dbSave(userSession)
}

func (userSession *UserSession) setUpdatedAt(date time.Time) {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.UpdatedAt = date
	dbSave(userSession)
}

func (userSession *UserSession) increaseNotifyCount() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.NotifyCount = userSession.NotifyCount + 1
	dbSave(userSession)
}

func (userSession *UserSession) resetNotifyCount() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.NotifyCount = 0
	dbSave(userSession)
}

func (userSession *UserSession) addStuff(item string, value string) {
	userSession.Lock()
	defer userSession.Unlock()

	if nil == userSession.Stuff {
		userSession.Stuff = make(map[string]string)
	}

	userSession.UpdatedAt = time.Now()
	userSession.Stuff[item] = value
	dbSave(userSession)
}

func (userSession *UserSession) clearStuff() {
	userSession.Lock()
	defer userSession.Unlock()

	userSession.Stuff = make(map[string]string)
	userSession.UpdatedAt = time.Now()

	dbSave(userSession)
}

func (userSession *UserSession) setWorking(flag bool) {
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
//	var session = &UserSession{}
//
//	db.View(func(tx *bolt.Tx) error {
//		b := tx.Bucket([]byte(sessionsBucketName))
//
//		v := b.Get([]byte(strconv.FormatInt(chatId, 10)))
//		fmt.Printf("The answer is: %s\n", v)
//
//		//session := new(UserSession)
//		err := json.Unmarshal(v, &session)
//
//		fmt.Println("error unmarshal", err)
//		fmt.Println("session", session)
//
//		return nil
//	})
//	fmt.Println("3444444444444444444444444444444444")
//
//	return session
//}

func loadSessions(fileName string) {
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

			sessions.users[chatId] = session
		}

		return nil
	})

	if nil != err {
		log.Println("Fill sessions error:", err)
		os.Exit(1)
	}

	for key, sess := range sessions.users {
		log.Printf("chat - %d - sess- %+v\n", key, sess)
	}
}
