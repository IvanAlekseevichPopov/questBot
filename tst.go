package main

import (
	"fmt"
	"time"
)

//const questStartLink = "first"
//const sessionsBucketName = "user_sessions"

func main() {
	sess1 := UserSession{
		IsWorkingWorking: false,
		UpdatedAt:        time.Now(),
		Position:         "111",
	}

	sess2 := UserSession{
		isWorking: true,
		UpdatedAt: time.Now(),
		Position:  "222",
	}

	sessions.set(1, sess1)
	sessions.set(2, sess2)

	fmt.Printf("%+v\n\n\n", sessions)

	sessions.get(1).Position = "333"

	tst()
}

func tst() {

	fmt.Printf("%+v\n", sessions.get(1))
	//fmt.Printf("%+v\n", sessions.get(2))
}
