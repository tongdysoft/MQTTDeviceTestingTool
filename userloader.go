package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	_ "net/http/pprof"
	"os"
)

func loadUserAuthFile(userFile string) *Auth {
	// if len(userFile) == 0 {
	// 	return new(auth.Allow)
	// }
	file, err := os.OpenFile(userFile, os.O_RDONLY, 0666)
	if err != nil {
		logPrint("X", fmt.Sprintf("%s %s %s: %s", lang("READFAIL"), lang("USERDATABASE"), userFile, err))
		return nil
	}
	userData, err := ioutil.ReadAll(file)
	if err != nil {
		logPrint("X", fmt.Sprintf("%s %s %s: %s", lang("READFAIL"), lang("USERDATABASE"), userFile, err))
		return nil
	}
	var userPermissions Auth
	json.Unmarshal(userData, &userPermissions)
	var auth *Auth = &userPermissions
	logPrint("I", fmt.Sprintf("%s %s: %d", lang("LOADED"), lang("USERDB"), len(userPermissions.Users)))
	logPrint("I", fmt.Sprintf("%s %s: %d", lang("LOADED"), lang("PERMDB"), len(userPermissions.AllowedTopics)))
	return auth
}

type Auth struct {
	Users         map[string]string
	AllowedTopics map[string][]string
}

func (a *Auth) Authenticate(user, password []byte) bool {
	if pass, ok := a.Users[string(user)]; ok && pass == string(password) {
		return true
	}

	return false
}

func (a *Auth) ACL(user []byte, topic string, write bool) bool {
	if topics, ok := a.AllowedTopics[string(user)]; ok {
		for _, t := range topics {
			if t == topic {
				return true
			}
		}

		return false
	}
	return true
}
