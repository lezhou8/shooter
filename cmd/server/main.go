package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

//////// server

const (
	maxPlayers     = 6
	maxTeamPlayers = maxPlayers >> 1
)

type messageHeaders byte // TODO move this to an internal module, shared between the client and server

const (
	nextRoundHeader messageHeaders = iota
	playerHeader
	locationsHeader
	shotHeader
	killedHeader
	teamPointHeader
	loseHealthHeader
	playerDisconnectHeader
)

type server struct {
	players           [maxPlayers]player
	teamAPoints       int
	teamBPoints       int
	round             int
	numPlayers        int
	currentNumPlayers int
	mutex             sync.Mutex
	broadcast         chan []byte
}

func newServer(numPlayers int) *server {
	return &server{
		numPlayers: numPlayers,
		broadcast:  make(chan []byte),
	}
}

const locationUpdateFrequency = 12

// broadcasting
func (server *server) run() {
	ticker := time.NewTicker(time.Second / locationUpdateFrequency)
	defer ticker.Stop()

	for {
		select {
		case broadcastMessage := <-server.broadcast:
			server.mutex.Lock()
			for _, player := range server.players {
				if !player.isEmpty() {
					if err := player.conn.WriteMessage(websocket.BinaryMessage, broadcastMessage); err != nil {
						log.Println(err)
					}
				}
			}
			server.mutex.Unlock()

		case <-ticker.C:
			// don't worry about locations before the game starts
			if server.round == 0 {
				break
			}

			// broadcast player locations
			locationsMessage := server.serialiseLocations()
			server.mutex.Lock()
			for _, player := range server.players {
				if !player.isEmpty() {
					if err := player.conn.WriteMessage(websocket.BinaryMessage, locationsMessage); err != nil {
						log.Println(err)
					}
				}
			}
			server.mutex.Unlock()
		}
	}
}

type clientMessage byte

const (
	hitMessage clientMessage = iota
	shotMessage
	locationMessage
)

func (server *server) serveWs(w http.ResponseWriter, r *http.Request) {
	// do not allow new connections if the lobby is full
	if server.numPlayers <= server.currentNumPlayers {
		http.Error(w, "Lobby is full", http.StatusForbidden)
		return
	}

	// do not allow new connections during active game
	if server.round > 0 {
		http.Error(w, "Game is in progress", http.StatusForbidden)
		return
	}

	// make websocket connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("server:", err)
		return
	}

	// properly induct the player into the game
	newPlayer, err := server.initialisePlayer(conn)
	if err != nil {
		log.Println(err)
		return
	}

	// go to next round if player quota reached
	if server.currentNumPlayers == server.numPlayers {
		server.nextRound()
	}

	// communication loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// graceful disconnect
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			log.Println(err)
			continue
		}

		// messaging errors
		if len(message) == 0 {
			log.Println("Empty message")
			continue
		}

		switch message[0] {
		case byte(hitMessage):
			if len(message) != 3 {
				log.Println("Incorrect message size for hit message")
				break
			}
			hitPlayerId := int(message[1])
			damage := int(message[2])

			// send to the specific player, that they got hit, detract health from them
			server.mutex.Lock() // TODO make a function specifically for this
			server.players[hitPlayerId].health -= damage
			if err := server.players[hitPlayerId].conn.WriteMessage(websocket.BinaryMessage, []byte{byte(loseHealthHeader), byte(damage)}); err != nil {
				log.Println(err)
			}
			server.mutex.Unlock()

			// check if the hit player is still alive, otherwise, broadcast to lobby
			if server.players[hitPlayerId].health < 1 {
				server.mutex.Lock()
				server.players[hitPlayerId].isAlive = false
				server.mutex.Unlock()

				// broadcast the kill
				server.broadcastByteMessage([]byte{byte(killedHeader), byte(newPlayer.id), byte(hitPlayerId)}) // TODO make a function specifically for this

				// if the whole team is dead then the round is done, the winning team gets a point
				if server.players[hitPlayerId].team == a && server.isTeamAAllDead() {
					server.broadcastByteMessage([]byte{byte(teamPointHeader), byte(b)})
					time.AfterFunc(roundEndGraceTime*time.Second, server.nextRound)
				} else if server.players[hitPlayerId].team == b && server.isTeamBAllDead() {
					server.broadcastByteMessage([]byte{byte(teamPointHeader), byte(a)})
					time.AfterFunc(roundEndGraceTime*time.Second, server.nextRound)
				}
			}

		case byte(shotMessage):
			// just broadcast shot, so each client can play a gunshot
			server.broadcastByteMessage([]byte{byte(shotHeader), byte(newPlayer.id)}) // TODO make a function specifically for this

		case byte(locationMessage):
			if len(message) != 4 {
				log.Println("Incorrect message size for location message")
				break
			}

			// just update location
			server.players[newPlayer.id].x = int8(message[1])
			server.players[newPlayer.id].y = int8(message[2])
			server.players[newPlayer.id].z = int8(message[3])

		default:
			log.Println("Invalid client message")
		}
	}

	// handle disconnect of player
	disconnectedPlayerId := newPlayer.id
	server.mutex.Lock()
	server.players[newPlayer.id] = player{}
	server.currentNumPlayers--
	server.mutex.Unlock()

	// inform lobby of player disconnection
	server.broadcastByteMessage([]byte{byte(playerDisconnectHeader), byte(disconnectedPlayerId)})
}

type successResponse int

const (
	success successResponse = iota
	failure
)

func (server *server) initialisePlayer(conn *websocket.Conn) (player, error) {
	// receive ID, team info
	_, idMessage, err := conn.ReadMessage()
	if err != nil {
		return player{}, err
	}

	// check for badly formed messages
	if len(idMessage) != 1 || idMessage[0] < 0 || idMessage[0] > 5 {
		// send the failure code
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{byte(failure)})
		return player{}, errors.New("Badly formed ID team message")
	}

	id := int(idMessage[0])

	// check that the requested player slot is free
	if !server.players[id].isEmpty() {
		// send the failure code
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{byte(failure)})
		return player{}, errors.New("Player slot is taken")
	}

	// player is okay to be inducted into game
	newPlayer := newPlayer(id, conn)
	server.mutex.Lock()
	server.players[id] = *newPlayer
	server.currentNumPlayers++
	server.mutex.Unlock()

	// send the success code
	if err = conn.WriteMessage(websocket.BinaryMessage, []byte{byte(success)}); err != nil {
		return *newPlayer, err
	}

	return *newPlayer, nil
}

func (server *server) cleanUp() {
	close(server.broadcast)
}

// check if all of team A is dead
func (server *server) isTeamAAllDead() bool {
	for _, player := range server.players[:maxTeamPlayers] {
		if !player.isEmpty() && player.isAlive {
			return false
		}
	}
	return true
}

// check if all of team B is dead
func (server *server) isTeamBAllDead() bool {
	for _, player := range server.players[maxTeamPlayers:] {
		if !player.isEmpty() && player.isAlive {
			return false
		}
	}
	return true
}

const (
	roundStartGraceTime = 8
	roundEndGraceTime   = 8
	lastRound           = 10 // TODO put in common internal shared file
	maxHealth           = 3
	afterGameLingerTime = 2
)

func (server *server) nextRound() {
	if server.round == lastRound {
		time.AfterFunc(afterGameLingerTime*time.Second, func() {
			server.cleanUp()
			os.Exit(0)
		})
	}

	// reset player attributes TODO make a function/method for this i.e. server.resetPlayers()
	server.mutex.Lock()
	for i := range server.players {
		player := &server.players[i]
		player.health = maxHealth
		player.isAlive = true
	}
	server.mutex.Unlock()

	server.broadcastByteMessage([]byte{byte(nextRoundHeader)}) // TODO make a function specifically for this

	server.mutex.Lock()
	server.round++
	server.mutex.Unlock()

	// send play message after some time
	time.AfterFunc(roundStartGraceTime*time.Second, func() {
		server.broadcastByteMessage([]byte{byte(playerHeader)}) // TODO make a function specifically for this
	})
}

type locationParcel struct {
	id      byte
	x, y, z int8
}

// turn location information into form that can be sent to clients
func (server *server) serialiseLocations() []byte {
	locationsBuffer := new(bytes.Buffer)

	// start with message type (location type message)
	if err := binary.Write(locationsBuffer, binary.LittleEndian, locationsHeader); err != nil {
		log.Println(err)
		return nil
	}

	// write each player's location data
	for _, player := range server.players {
		if player.isEmpty() {
			continue
		}
		if err := binary.Write(locationsBuffer, binary.LittleEndian, locationParcel{byte(player.id), player.x, player.y, player.z}); err != nil {
			log.Println(err)
			return nil
		}
	}

	return locationsBuffer.Bytes()
}

func (server *server) broadcastByteMessage(message []byte) {
	server.broadcast <- message
}

//////// player

type team int

const (
	a team = iota
	b
)

type player struct {
	id, health int
	team
	conn    *websocket.Conn
	isAlive bool
	x, y, z int8
}

func newPlayer(id int, conn *websocket.Conn) *player {
	var team team
	if id < maxTeamPlayers {
		team = a
	} else {
		team = b
	}
	return &player{
		id:   id,
		team: team,
		conn: conn,
	}
}

func (player *player) isEmpty() bool {
	return player.conn == nil
}

//////// program entry

func main() {
	// commandline arguments
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s [port] [num-players]\n", os.Args[0])
		return
	}

	portString := os.Args[1]
	numPlayersString := os.Args[2]

	port, err := strconv.Atoi(portString)
	if err != nil {
		fmt.Println("Port needs to be a number:", err)
		return
	}

	numPlayers, err := strconv.Atoi(numPlayersString)
	if err != nil {
		fmt.Println("num-players needs to be a number:", err)
		return
	}

	if numPlayers < 1 || maxPlayers < numPlayers {
		fmt.Println("num-players must be between 1 and 6, inclusive")
		return
	}

	// start server
	server := newServer(numPlayers)
	defer server.cleanUp()
	go server.run()
	http.HandleFunc("/ws", server.serveWs)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", port), nil))
}
