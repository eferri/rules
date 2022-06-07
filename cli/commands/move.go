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
		req := state.Request

		// Convert API settings to map params
		params := map[string]string{
			rules.ParamFoodSpawnChance:     fmt.Sprint(req.Game.Ruleset.Settings.FoodSpawnChance),
			rules.ParamMinimumFood:         fmt.Sprint(req.Game.Ruleset.Settings.MinimumFood),
			rules.ParamHazardDamagePerTurn: fmt.Sprint(req.Game.Ruleset.Settings.HazardDamagePerTurn),
			rules.ParamShrinkEveryNTurns:   fmt.Sprint(req.Game.Ruleset.Settings.RoyaleSettings.ShrinkEveryNTurns),
		}

		ruleset := rules.NewRulesetBuilder().WithSeed(0).WithParams(params).NamedRuleset(req.Game.Ruleset.Name)
		mapID := req.Game.Map

		gameMap, err := maps.GetMap(mapID)
		if err != nil {
			errLog.Fatalf("Error getting map: %v", err)
		}

		settings := ruleset.Settings().WithRand(rules.MaxRand)

		snakeMap := map[string]client.Snake{}
		youID := req.You.ID
		youIdx := 0

		snakeIds := []string{}

		for _, s := range req.Board.Snakes {
			snakeIds = append(snakeIds, s.ID)
		}

		// Initialize board, boardState
		boardState, err := maps.SetupBoard(mapID, settings, req.Board.Width, req.Board.Height, snakeIds)
		if err != nil {
			errLog.Fatalf("Error producing next board state: %v", err)
		}

		for i, s := range req.Board.Snakes {
			boardState.Snakes[i].Health = s.Health
			boardState.Snakes[i].Body = PointFromCoordArray(s.Body)
		}

		boardState.Turn = req.Turn
		boardState.Food = PointFromCoordArray(req.Board.Food)
		boardState.Hazards = PointFromCoordArray(req.Board.Hazards)

		moves := []rules.SnakeMove{}

		for i, move := range state.Moves {
			moves = append(moves, rules.SnakeMove{
				ID:   req.Board.Snakes[i].ID,
				Move: move,
			})
		}

		boardState, err = maps.PreUpdateBoard(gameMap, boardState, settings)
		if err != nil {
			errLog.Fatalf("Error PreUpdateBoard: %v", err)
		}

		_, boardState, err = ruleset.Execute(boardState, moves)
		if err != nil {
			errLog.Fatalf("Error executing move: %v", err)
		}

		boardState, err = maps.PostUpdateBoard(gameMap, boardState, settings)
		if err != nil {
			errLog.Fatalf("Error PreUpdateBoard: %v", err)
		}

		newBoard := client.Board{
			Height:  boardState.Height,
			Width:   boardState.Width,
			Food:    client.CoordFromPointArray(boardState.Food),
			Hazards: client.CoordFromPointArray(boardState.Hazards),
			Snakes:  convertRulesAPISnakes(boardState.Snakes, snakeMap),
		}

		newRequest := client.SnakeRequest{
			Game:  req.Game,
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
