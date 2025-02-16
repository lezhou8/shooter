package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	rl "github.com/gen2brain/raylib-go/raylib"
)

func main() {
	// command-line arguments
	if len(os.Args) != 4 {
		fmt.Printf("Usage: %s [IP] [port] [ID]\n", os.Args[0])
		return
	}

	ip := os.Args[1]
	portString := os.Args[2]
	idString := os.Args[3]

	port, err := strconv.Atoi(portString)
	if err != nil {
		fmt.Println("Port needs to be a number:", err)
		return
	}

	id, err := strconv.Atoi(idString)
	if err != nil {
		fmt.Println("ID needs to be a number:", err)
		return
	}

	if id < 0 || maxPlayers - 1 < id {
		fmt.Println("ID must be between 0 and 5, inclusive")
		return
	}

	// establish connection
	meta := newMeta(id)
	if err := meta.connectToServer(fmt.Sprintf("ws://%s:%d/ws", ip, port)); err != nil {
		log.Fatal(err)
	}

	// initialise game
	rl.SetTraceLogLevel(rl.LogNone)
	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(0, 0, "shooter")
	defer rl.CloseWindow()
	rl.SetWindowMinSize(internalWindowWidth, internalWindowHeight)
	rl.SetTargetFPS(30)
	rl.DisableCursor()

	// load resources
	resources := resources{}
	resources.loadResources()
	defer resources.unloadResources()

	// screen rectangles
	internalWindowRectangle := rl.Rectangle{
		X:      0,
		Y:      0,
		Width:  float32(resources.textures.renderTexture.Texture.Width),
		Height: float32(-resources.textures.renderTexture.Texture.Height),
	}
	destinationRectangle := calculateScreenRectangle()

	// game objects
	playerWorld := newPlayerWorld(&resources, meta)
	defer playerWorld.cleanUp()
	defer disconnect(playerWorld.conn)
	context, cancel := context.WithCancel(context.Background())
	go playerWorld.receiveMessages(context)

	// wait until the game starts before we make a window
	playerWorld.waitUntilGameStarts()

	go playerWorld.sendServerLocation()

	// game loop
	for !rl.WindowShouldClose() {
		// update
		playerWorld.update()

		// exit if requested
		if playerWorld.exitRequested {
			break
		}

		// draw to render texture
		rl.BeginTextureMode(resources.renderTexture)
		playerWorld.draw()
		rl.EndTextureMode()

		// recalculate screen output rectangle if screen dimensions changed
		if rl.IsWindowResized() {
			destinationRectangle = calculateScreenRectangle()
		}

		// draw to screen
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)
		rl.BeginShaderMode(resources.chromaticAberration)
		rl.DrawTexturePro(resources.renderTexture.Texture, internalWindowRectangle, destinationRectangle, rl.Vector2Zero(), 0, rl.White)
		rl.EndShaderMode()
		rl.EndDrawing()
	}

	// close the message receiver
	cancel()

	// print result to console
	switch {
	case playerWorld.teamAPoints == playerWorld.teamBPoints:
		fmt.Println("  DRAW")
	case playerWorld.team == a && playerWorld.teamAPoints > playerWorld.teamBPoints:
		fmt.Println("  CONGRATULATIONS::TEAM A WON")
	case playerWorld.team == a && playerWorld.teamAPoints < playerWorld.teamBPoints:
		fmt.Println("  DEFEAT::TEAM B WON")
	case playerWorld.team == b && playerWorld.teamBPoints > playerWorld.teamAPoints:
		fmt.Println("  CONGRATULATIONS::TEAM B WON")
	case playerWorld.team == b && playerWorld.teamBPoints < playerWorld.teamAPoints:
		fmt.Println("  DEFEAT::TEAM A WON")
	}
	fmt.Printf("  TEAM A POINTS::%d\n", playerWorld.teamAPoints)
	for i, otherPlayer := range playerWorld.otherPlayers[:maxTeamPlayers] {
		if i == playerWorld.id {
			fmt.Printf("> %d KILLS: %d, DEATHS: %d\n", i, playerWorld.killAmount, playerWorld.deathAmount)
		} else {
			fmt.Printf("  %d KILLS: %d, DEATHS: %d\n", i, otherPlayer.killAmount, otherPlayer.deathAmount)
		}
	}
	fmt.Printf("  TEAM B POINTS::%d\n", playerWorld.teamBPoints)
	for i, otherPlayer := range playerWorld.otherPlayers[maxTeamPlayers:] {
		if i + maxTeamPlayers == playerWorld.id {
			fmt.Printf("> %d KILLS: %d, DEATHS: %d\n", i + maxTeamPlayers, playerWorld.killAmount, playerWorld.deathAmount)
		} else {
			fmt.Printf("  %d KILLS: %d, DEATHS: %d\n", i + maxTeamPlayers, otherPlayer.killAmount, otherPlayer.deathAmount)
		}
	}
}

func calculateScreenRectangle() rl.Rectangle {
	scale := min(float32(rl.GetScreenWidth())/internalWindowWidth, float32(rl.GetScreenHeight())/internalWindowHeight)
	rectangle := rl.Rectangle{
		X:      (float32(rl.GetScreenWidth()) - float32(internalWindowWidth)*scale) * 0.5,
		Y:      (float32(rl.GetScreenHeight()) - float32(internalWindowHeight)*scale) * 0.5,
		Width:  internalWindowWidth * scale,
		Height: internalWindowHeight * scale,
	}
	return rectangle
}
