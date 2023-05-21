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
	logPrint("i", fmt.Sprintf("%s %s: %d", lang("LOADED"), lang("USERDB"), len(userPermissions.Users)))
	logPrint("i", fmt.Sprintf("%s %s: %d", lang("LOADED"), lang("PERMDB"), len(userPermissions.AllowedTopics)))
	return auth
}

// Auth is an example auth provider for the server. In the real world
// you are more likely to replace these fields with database/cache lookups
// to check against an auth list. As the Auth Controller is an interface, it can
// be built however you want, as long as it fulfils the interface signature.
type Auth struct {
	Users         map[string]string   // A map of usernames (key) with passwords (value).
	AllowedTopics map[string][]string // A map of usernames and topics
}

// Authenticate returns true if a username and password are acceptable.
func (a *Auth) Authenticate(user, password []byte) bool {
	// If the user exists in the auth users map, and the password is correct,
	// then they can connect to the server. In the real world, this could be a database
	// or cached users lookup.
	if pass, ok := a.Users[string(user)]; ok && pass == string(password) {
		return true
	}

	return false
}

// ACL returns true if a user has access permissions to read or write on a topic.
func (a *Auth) ACL(user []byte, topic string, write bool) bool {

	// An example ACL - if the user has an entry in the auth allow list, then they are
	// subject to ACL restrictions. Only let them use a topic if it's available for their
	// user.
	if topics, ok := a.AllowedTopics[string(user)]; ok {
		for _, t := range topics {

			// In the real world you might allow all topics prefixed with a user's username,
			// or similar multi-topic filters.
			if t == topic {
				return true
			}
		}

		return false
	}

	// Otherwise, allow all topics.
	return true
}
