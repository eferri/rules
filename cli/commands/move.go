package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/BattlesnakeOfficial/rules"
	"github.com/BattlesnakeOfficial/rules/client"
	"github.com/BattlesnakeOfficial/rules/maps"
	"github.com/spf13/cobra"
)

type MoveState struct {
	Request client.SnakeRequest `json:"request"`
	Moves   []string            `json:"moves"`
}

func NewMoveCommand() *cobra.Command {
	var playCmd = &cobra.Command{
		Use:   "move",
		Short: "Apply moves to the API request from stdin",
		Long:  "Apply moves to the API request from stdin. Print results to stdout",
		Run: func(cmd *cobra.Command, args []string) {
			move()
		},
	}

	return playCmd
}

func move() {
	decoder := json.NewDecoder(os.Stdin)

	errLog := log.New(os.Stderr, "", 0)

	for {
		var state MoveState
		err := decoder.Decode(&state)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			errLog.Print(err)
			break
		}

		// Convert API settings to map params
		params := map[string]string{
			rules.ParamGameType:            state.Request.Game.Ruleset.Name,
			rules.ParamFoodSpawnChance:     fmt.Sprint(state.Request.Game.Ruleset.Settings.FoodSpawnChance),
			rules.ParamMinimumFood:         fmt.Sprint(state.Request.Game.Ruleset.Settings.MinimumFood),
			rules.ParamHazardDamagePerTurn: fmt.Sprint(state.Request.Game.Ruleset.Settings.HazardDamagePerTurn),
			rules.ParamShrinkEveryNTurns:   fmt.Sprint(state.Request.Game.Ruleset.Settings.RoyaleSettings.ShrinkEveryNTurns),
		}

		ruleset := rules.NewRulesetBuilder().WithSeed(0).WithParams(params).Ruleset()
		mapID := state.Request.Game.Map
		settings := ruleset.Settings()

		width := state.Request.Board.Width
		height := state.Request.Board.Height

		snakeMap := map[string]client.Snake{}
		youID := state.Request.You.ID
		youIdx := 0

		for i, s := range state.Request.Board.Snakes {
			snakeMap[s.ID] = s

			if s.ID == youID {
				youIdx = i
			}
		}

		// Initialize board, boardState
		boardState := rules.NewBoardState(width, height)

		for _, s := range state.Request.Board.Snakes {
			boardState.Snakes = append(boardState.Snakes, rules.Snake{
				ID:     s.ID,
				Health: s.Health,
				Body:   PointFromCoordArray(s.Body),
			})
		}

		boardState.Turn = state.Request.Turn
		boardState.Food = PointFromCoordArray(state.Request.Board.Food)
		boardState.Hazards = PointFromCoordArray(state.Request.Board.Hazards)

		moves := []rules.SnakeMove{}

		for i, move := range state.Moves {
			moves = append(moves, rules.SnakeMove{
				ID:   state.Request.Board.Snakes[i].ID,
				Move: move,
			})
		}
		boardState, err = ruleset.CreateNextBoardState(boardState, moves)
		if err != nil {
			errLog.Fatalf("Error producing next board state: %v", err)
		}

		boardState, err = maps.UpdateBoard(mapID, boardState, settings)
		if err != nil {
			errLog.Fatalf("Error updating board with game map: %v", err)
		}

		_, err = ruleset.IsGameOver(boardState)
		if err != nil {
			errLog.Fatalf("Error IsGameOver: %s", err)
		}

		newBoard := client.Board{
			Height:  boardState.Height,
			Width:   boardState.Width,
			Food:    client.CoordFromPointArray(boardState.Food),
			Hazards: client.CoordFromPointArray(boardState.Hazards),
			Snakes:  convertRulesAPISnakes(boardState.Snakes, snakeMap),
		}

		newRequest := client.SnakeRequest{
			Game:  state.Request.Game,
			Turn:  boardState.Turn + 1,
			Board: newBoard,
			You:   convertRulesAPISnake(boardState.Snakes[youIdx], snakeMap[youID]),
		}

		newRequestJson, err := json.Marshal(newRequest)
		if err != nil {
			errLog.Fatalf("Error marshalling: %v", err)
		}

		os.Stdout.Write(newRequestJson)
	}
}

func convertRulesAPISnake(snake rules.Snake, snakeState client.Snake) client.Snake {
	var health int
	body := client.CoordFromPointArray(snake.Body)

	if snake.EliminatedCause != rules.NotEliminated {
		health = 0
	} else {
		health = snake.Health
	}

	return client.Snake{
		ID:             snake.ID,
		Name:           snakeState.Name,
		Health:         health,
		Body:           body,
		Latency:        snakeState.Latency,
		Head:           body[0],
		Length:         int(len(snake.Body)),
		Shout:          snakeState.Shout,
		Customizations: snakeState.Customizations,
	}
}

func convertRulesAPISnakes(snakes []rules.Snake, snakeStates map[string]client.Snake) []client.Snake {
	a := make([]client.Snake, 0)
	for _, snake := range snakes {
		a = append(a, convertRulesAPISnake(snake, snakeStates[snake.ID]))
	}
	return a
}

func PointFromCoord(crd client.Coord) rules.Point {
	return rules.Point{X: crd.X, Y: crd.Y}
}

func PointFromCoordArray(coordArray []client.Coord) []rules.Point {
	a := make([]rules.Point, 0)
	for _, crd := range coordArray {
		a = append(a, PointFromCoord(crd))
	}
	return a
}
