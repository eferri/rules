package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/BattlesnakeOfficial/rules/client"
	"github.com/gorilla/websocket"
)

type BoardGame struct {
	ID      string         `json:"id"`
	Width   int            `json:"Width"`
	Height  int            `json:"Height"`
	Ruleset client.Ruleset `json:"Ruleset"`
}

type BoardCoord struct {
	X int `json:"X"`
	Y int `json:"Y"`
}

type BoardResponse struct {
	Game BoardGame `json:"Game"`
}

type BoardDeath struct {
	Cause        string `json:"Cause"`
	Turn         int32  `json:"Turn"`
	EliminatedBy string `json:"EliminatedBy"`
}

type BoardBattlesnake struct {
	Body     []BoardCoord `json:"Body"`
	Color    string       `json:"Color"`
	ID       string       `json:"ID"`
	Name     string       `json:"Name"`
	Health   int          `json:"Health"`
	Latency  int32        `json:"Latency"`
	Death    *BoardDeath  `json:"Death"`
	HeadType string       `json:"HeadType"`
	TailType string       `json:"TailType"`
	Author   string       `json:"Author"`
	Shout    string       `json:"Shout"`
}

type Frame struct {
	Snakes  []BoardBattlesnake `json:"Snakes"`
	Turn    int                `json:"Turn"`
	Food    []BoardCoord       `json:"Food"`
	Hazards []BoardCoord       `json:"Hazards"`
}

type WsMessage struct {
	Data *Frame `json:"Data"`
	Type string `json:"Type"`
}

func BoardCoordFromPointArray(ptArray []rules.Point) []BoardCoord {
	a := make([]BoardCoord, 0)
	for _, pt := range ptArray {
		a = append(a, BoardCoord{pt.X, pt.Y})
	}
	return a
}

func frameFromState(state *rules.BoardState, snakeStates map[string]SnakeState) Frame {
	websiteSnakes := make([]BoardBattlesnake, 0)
	for _, snake := range state.Snakes {
		var death *BoardDeath
		if snake.EliminatedCause == rules.NotEliminated && snake.EliminatedBy == "" {
			death = nil
		} else {
			death = &BoardDeath{
				EliminatedBy: snake.EliminatedBy,
				Cause:        snake.EliminatedCause,
			}
		}

		wSnake := BoardBattlesnake{
			Body:     BoardCoordFromPointArray(snake.Body),
			Color:    snakeStates[snake.ID].Color,
			ID:       snake.ID,
			Name:     snakeStates[snake.ID].Name,
			Health:   snake.Health,
			Latency:  0,
			Death:    death,
			HeadType: snakeStates[snake.ID].Head,
			TailType: snakeStates[snake.ID].Tail,
			Author:   "",
			Shout:    "",
		}
		websiteSnakes = append(websiteSnakes, wSnake)
	}

	frame := Frame{
		Snakes:  websiteSnakes,
		Turn:    state.Turn,
		Food:    BoardCoordFromPointArray(state.Food),
		Hazards: BoardCoordFromPointArray(state.Hazards),
	}
	return frame
}

func serveGameId(writer http.ResponseWriter, request *http.Request, state *GameState) {
	if request.Method != "GET" {
		http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Return game
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	responseJson, err := json.Marshal(BoardResponse{
		Game: BoardGame{
			ID:     state.gameID,
			Height: state.Height,
			Width:  state.Width,
			Ruleset: client.Ruleset{
				Name: state.ruleset.Name(),
			},
		},
	})

	if err != nil {
		log.Fatal(err)
	}
	_, err = writer.Write(responseJson)
	if err != nil {
		log.Fatal(err)
	}
}

type wsReq struct {
	frameChannel chan Frame
	doneChannel  chan struct{}
}

func serveFrames(writer http.ResponseWriter, request *http.Request, req wsReq) {
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

	// Read and discard messages for duration of websocket connection
	go func() {
		for {
			if _, _, err := ws.NextReader(); err != nil {
				break
			}
		}
		closedChannel <- struct{}{}
	}()

	var lastFrame *Frame = nil

conn_loop:
	for {
		select {
		case frame, game_running := <-req.frameChannel:
			_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))

			var frameJson []byte
			var err error
			if game_running {
				lastFrame = &frame
				frameJson, err = json.Marshal(WsMessage{&frame, "frame"})
			} else {
				frameJson, err = json.Marshal(WsMessage{lastFrame, "game_end"})
			}

			if err != nil {
				log.Panicf("[PANIC] error marshaling frame: %s", err)
			}
			err = ws.WriteMessage(1, frameJson)
			if err != nil {
				log.Printf("[WARN] error writing websocket message: %s", err)
				break conn_loop
			}

			if !game_running {
				err = ws.WriteMessage(websocket.CloseMessage, []byte{})
				if err != nil {
					log.Printf("[WARN] error closing websocket: %s", err)
				}
				break conn_loop
			}

		case <-closedChannel:
			break conn_loop
		}
	}
	req.doneChannel <- struct{}{}
}

type BoardServer struct {
	http.Server
	debugRequests bool
	frameChannel  chan Frame
	doneChannel   chan struct{}
}

func NewBoardServer(debugRequests bool) *BoardServer {

	return &BoardServer{
		Server:        http.Server{},
		debugRequests: debugRequests,
		frameChannel:  make(chan Frame),
		doneChannel:   make(chan struct{}),
	}
}

func (b *BoardServer) startBoardServer(addr string, state *GameState) {
	engineHostName := url.QueryEscape(fmt.Sprintf("http://localhost%s", addr))
	log.Printf("View board at http://127.0.0.1:3000/?engine=%s&game=%s", engineHostName, state.gameID)

	mux := http.NewServeMux()

	gamePath := fmt.Sprintf("/games/%s", state.gameID)
	mux.HandleFunc(gamePath, func(w http.ResponseWriter, r *http.Request) {
		if b.debugRequests {
			log.Printf("%s: %s", r.Method, gamePath)
		}
		serveGameId(w, r, state)
	})

	var registerWsReqChannel = make(chan wsReq)

	socketPath := fmt.Sprintf("/games/%s/events", state.gameID)
	mux.HandleFunc(socketPath, func(w http.ResponseWriter, r *http.Request) {
		socketReq := wsReq{
			frameChannel: make(chan Frame, 100),
			doneChannel:  make(chan struct{}),
		}
		registerWsReqChannel <- socketReq
		if b.debugRequests {
			log.Printf("%s: %s New websocket connection", r.Method, socketPath)
		}
		serveFrames(w, r, socketReq)
	})

	// Send frames to websocket connections
	go func() {
		var boardFrameBuffer []Frame
		var reqMap = make(map[wsReq]int)

	serve_loop:
		for {
			select {
			case board, open := <-b.frameChannel:
				if open {
					boardFrameBuffer = append(boardFrameBuffer, board)
				} else {
					b.frameChannel = nil
				}
			case websocketChannels := <-registerWsReqChannel:
				// Register a new websocket connection
				reqMap[websocketChannels] = 0
			case <-b.doneChannel:
				break serve_loop
			}

			for req, frameCount := range reqMap {
				// If websocket connection is closed, remove it from the map of registered connections
				select {
				case <-req.doneChannel:
					delete(reqMap, req)
					continue
				default:
				}

				for i, frame := range boardFrameBuffer[frameCount:] {
					// Send frames to websocket connection without blocking, to avoid being affected
					// by a slow connection
					select {
					case req.frameChannel <- frame:
						reqMap[req] = frameCount + i + 1
					default:
					}
				}
				if b.frameChannel == nil && reqMap[req] >= len(boardFrameBuffer) {
					close(req.frameChannel)
				}
			}
		}
		b.doneChannel <- struct{}{}
	}()

	b.Handler = mux
	b.Addr = addr

	go func() {
		_ = b.ListenAndServe()
		b.doneChannel <- struct{}{}
	}()
}

func (b *BoardServer) sendState(state *rules.BoardState, snakeStates map[string]SnakeState) {
	frame := frameFromState(state, snakeStates)
	b.frameChannel <- frame
}

func (b *BoardServer) gameOver() {
	close(b.frameChannel)
}

func (b *BoardServer) stop() {
	b.doneChannel <- struct{}{}
	err := b.Server.Shutdown(context.Background())
	if err != nil {
		log.Print(err)
	}
	<-b.doneChannel
	<-b.doneChannel
}
