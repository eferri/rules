package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/gorilla/websocket"
)

type WebsiteCoord struct {
	X int32 `json:"X"`
	Y int32 `json:"Y"`
}

type Ruleset struct {
	Name string `json:"name"`
}

type GameWebsite struct {
	Height       int32   `json:"Height"`
	Width        int32   `json:"Width"`
	Ruleset      Ruleset `json:"Ruleset"`
	Status       string  `json:"Status"`
	Id           string  `json:"ID"`
	SnakeTimeout int32   `json:"SnakeTimeout"`
}

type GameWebsiteResponse struct {
	Game      GameWebsite `json:"Game"`
	LastFrame BoardFrame  `json:"LastFrame"`
}

type WebsiteBattlesnakeDeath struct {
	Cause        string `json:"Cause"`
	Turn         int32  `json:"Turn"`
	EliminatedBy string `json:"EliminatedBy"`
}

type WebsiteBattlesnake struct {
	Body     []WebsiteCoord           `json:"Body"`
	Color    string                   `json:"Color"`
	ID       string                   `json:"ID"`
	Name     string                   `json:"Name"`
	Health   int32                    `json:"Health"`
	Latency  int32                    `json:"Latency"`
	Death    *WebsiteBattlesnakeDeath `json:"Death"`
	HeadType string                   `json:"HeadType"`
	TailType string                   `json:"TailType"`
	Squad    string                   `json:"Squad"`
	Author   string                   `json:"Author"`
	Shout    string                   `json:"Shout"`
}

type BoardFrame struct {
	Snakes  []WebsiteBattlesnake `json:"Snakes"`
	Turn    int32                `json:"Turn"`
	Food    []WebsiteCoord       `json:"Food"`
	Hazards []WebsiteCoord       `json:"Hazards"`
}

func websiteCoordFromPoint(pt rules.Point) WebsiteCoord {
	return WebsiteCoord{X: pt.X, Y: pt.Y}
}

func websiteCoordFromPointArray(ptArray []rules.Point) []WebsiteCoord {
	a := make([]WebsiteCoord, 0)
	for _, pt := range ptArray {
		a = append(a, websiteCoordFromPoint(pt))
	}
	return a
}

func stateToBoardFrame(ruleset string, state *rules.BoardState, outOfBounds []rules.Point, snakes []Battlesnake, turn int32) BoardFrame {
	websiteSnakes := make([]WebsiteBattlesnake, 0)
	for _, snake := range state.Snakes {
		var death *WebsiteBattlesnakeDeath
		if snake.EliminatedCause == rules.NotEliminated && snake.EliminatedBy == "" {
			death = nil
		} else {
			death = &WebsiteBattlesnakeDeath{
				EliminatedBy: snake.EliminatedBy,
				Cause:        snake.EliminatedCause,
			}
		}

		wSnake := WebsiteBattlesnake{
			Body:     websiteCoordFromPointArray(snake.Body),
			Color:    Battlesnakes[snake.ID].Color,
			ID:       snake.ID,
			Name:     Battlesnakes[snake.ID].Name,
			Health:   snake.Health,
			Latency:  0,
			Death:    death,
			HeadType: Battlesnakes[snake.ID].Head,
			TailType: Battlesnakes[snake.ID].Tail,
			Squad:    Battlesnakes[snake.ID].Squad,
			Author:   "",
			Shout:    "",
		}
		websiteSnakes = append(websiteSnakes, wSnake)
	}

	frame := BoardFrame{
		Snakes:  websiteSnakes,
		Turn:    turn,
		Food:    websiteCoordFromPointArray(state.Food),
		Hazards: websiteCoordFromPointArray(outOfBounds),
	}
	return frame
}

type gameChannel chan BoardFrame

func serveGameId(writer http.ResponseWriter, request *http.Request, lastFrame BoardFrame, rulesetName string) {
	if request.Method != "GET" {
		http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path_pieces := strings.SplitN(request.URL.Path, "/", 3)
	if len(path_pieces) != 3 || path_pieces[2] != GameId {
		http.NotFound(writer, request)
		return
	}

	// Return game
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	responseJson, err := json.Marshal(GameWebsiteResponse{
		Game: GameWebsite{
			Height:       Height,
			Width:        Width,
			Ruleset:      Ruleset{Name: rulesetName},
			Status:       "running",
			Id:           GameId,
			SnakeTimeout: Timeout,
		},
		LastFrame: lastFrame,
	})

	if err != nil {
		log.Fatal(err)
	}
	_, err = writer.Write(responseJson)
	if err != nil {
		log.Fatal(err)
	}
}

type wsChannels struct {
	frameChannel chan BoardFrame
	doneChannel  chan struct{}
}

func serveBoardFrames(writer http.ResponseWriter, request *http.Request, wc wsChannels) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	ws, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	closedChannel := make(chan struct{})

	// Read and discard messages from websocket connection
	go func() {
		for {
			if _, _, err := ws.NextReader(); err != nil {
				break
			}
		}
		closedChannel <- struct{}{}
	}()

conn_loop:
	for {
		select {
		case frame, open := <-wc.frameChannel:
			ws.SetWriteDeadline(time.Now().Add(10 * time.Second))

			if !open {
				ws.WriteMessage(websocket.CloseMessage, []byte{})
				break conn_loop
			}
			frameJson, err := json.Marshal(frame)
			if err != nil {
				log.Println(err)
				break conn_loop
			}
			err = ws.WriteMessage(1, frameJson)
			if err != nil {
				log.Printf("error writing message: %s", err)
				break conn_loop
			}
		case <-closedChannel:
			break conn_loop
		}
	}
	wc.doneChannel <- struct{}{}
}

type boardServer struct {
	http.Server
	frameChannel chan BoardFrame
	doneChannel  chan struct{}
}

func startBoardServer(rulesetName string) *boardServer {
	serveURL := ":4000"
	engineHostNameQuery := url.QueryEscape(fmt.Sprintf("http://localhost%s", serveURL))
	log.Printf("View board at http://localhost:3000/?engine=%s&game=%s", engineHostNameQuery, GameId)

	var registerGameReqChannel = make(chan gameChannel)

	handler := http.NewServeMux()

	handler.HandleFunc("/games/", func(writer http.ResponseWriter, request *http.Request) {
		lastFrameChannel := make(chan BoardFrame)
		registerGameReqChannel <- lastFrameChannel
		lastFrame := <-lastFrameChannel
		serveGameId(writer, request, lastFrame, rulesetName)
	})

	var registerWsReqChannel = make(chan wsChannels)

	handler.HandleFunc(fmt.Sprintf("/socket/%s", GameId), func(writer http.ResponseWriter, request *http.Request) {
		frameChannel := make(chan BoardFrame, 100)
		doneChannel := make(chan struct{})
		reqChannels := wsChannels{frameChannel, doneChannel}
		registerWsReqChannel <- reqChannels
		serveBoardFrames(writer, request, reqChannels)
	})

	frameChannel := make(chan BoardFrame)
	doneChannel := make(chan struct{})

	// Send frames to websocket connections
	go func() {
		var boardFrameBuffer []BoardFrame
		var reqMap = make(map[wsChannels]int32)

		gameOver := false

		for {
			if !gameOver {
				select {
				case board, open := <-frameChannel:
					if open {
						boardFrameBuffer = append(boardFrameBuffer, board)
					} else {
						gameOver = true
					}
				case lastFrameChannel := <-registerGameReqChannel:
					lastFrameChannel <- boardFrameBuffer[len(boardFrameBuffer)-1]
				case websocketChannels := <-registerWsReqChannel:
					// Register a new websocket connection
					reqMap[websocketChannels] = 0
				}
			}

			allReqDone := true

			for wc, frameCount := range reqMap {
				// If websocket connection is closed, remove it from the map of registered connections
				select {
				case <-wc.doneChannel:
					delete(reqMap, wc)
					continue
				default:
				}

				for i := frameCount; i < int32(len(boardFrameBuffer)); i++ {
					// Send frames to websocket connection without blocking, to avoid being affected
					// by a slow connection
					select {
					case wc.frameChannel <- boardFrameBuffer[i]:
						reqMap[wc] = i
					default:
					}
					allReqDone = false
				}
				if gameOver && reqMap[wc] >= int32(len(boardFrameBuffer)) {
					close(wc.frameChannel)
				}
			}

			if gameOver && allReqDone {
				break
			} else if gameOver {
				// If the game is over, poll existing connections until all frames are sent
				// or the connection is closed
				time.Sleep(time.Duration(100) * time.Millisecond)
			}
		}
		doneChannel <- struct{}{}
	}()

	server := &boardServer{http.Server{Addr: serveURL, Handler: handler}, frameChannel, doneChannel}

	go func() {
		server.ListenAndServe()
		doneChannel <- struct{}{}
	}()

	return server
}

func (b *boardServer) stop() {
	close(b.frameChannel)
	b.Server.Shutdown(context.Background())
	<-b.doneChannel
	<-b.doneChannel
}
