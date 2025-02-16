package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gorilla/websocket"
)

//////// playerWorld
//////// manages the player including its interaction with the world

const (
	moveSpeed                      = 1
	slowMoveSpeed                  = 0.3
	jumpSpeed                      = 1.2
	gravity                        = -3.5
	accurateMovementSpeedThreshold = 0.1
	swapTime                       = 2
	maxHealth                      = 3
)

var inaccuracySkew = rl.Vector3{X: 0.6, Y: 0.7, Z: 0.4}

type playerWorld struct {
	player
	world
	otherPlayerManager
	*meta
	exitRequested bool
}

func newPlayerWorld(resources *resources, meta *meta) *playerWorld {
	return &playerWorld{
		player:             *newPlayer(resources),
		world:              *newWorld(resources),
		otherPlayerManager: *newOtherPlayerManager(resources),
		meta:               meta,
	}
}

// takes responsibility of player movement to handle collisions
func (playerWorld *playerWorld) update() {
	// look around
	mouseDelta := rl.GetMouseDelta()
	rl.CameraYaw(&playerWorld.camera, -mouseDelta.X*playerWorld.lookSensitivity, 0)
	rl.CameraPitch(&playerWorld.camera, -mouseDelta.Y*playerWorld.lookSensitivity, 1, 0, 0)

	// statistics board
	if rl.IsKeyDown(rl.KeyTab) {
		playerWorld.statisticsBoardRequested = true
	} else {
		playerWorld.statisticsBoardRequested = false
	}

	// do not allow movement or shooting if in limbo
	if playerWorld.playerState == limbo {
		return
	}

	// input
	move := rl.Vector3Zero()
	if rl.IsKeyDown(rl.KeyW) {
		move = rl.Vector3Add(move, rl.GetCameraForward(&playerWorld.camera))
	}
	if rl.IsKeyDown(rl.KeyS) {
		move = rl.Vector3Subtract(move, rl.GetCameraForward(&playerWorld.camera))
	}
	if rl.IsKeyDown(rl.KeyD) {
		move = rl.Vector3Add(move, rl.GetCameraRight(&playerWorld.camera))
	}
	if rl.IsKeyDown(rl.KeyA) {
		move = rl.Vector3Subtract(move, rl.GetCameraRight(&playerWorld.camera))
	}

	// speed
	var speed float32
	if rl.IsKeyDown(rl.KeyLeftShift) {
		speed = slowMoveSpeed
	} else {
		speed = moveSpeed
	}
	deltaTime := rl.GetFrameTime()
	move.Y = 0
	move = rl.Vector3Scale(rl.Vector3Normalize(move), speed*deltaTime)
	playerWorld.velocity = rl.Vector3Add(playerWorld.velocity, move)

	// damping
	playerWorld.velocity = rl.Vector3Scale(playerWorld.velocity, 1.0/(1.0+deltaTime*5))

	// vertical movement
	playerWorld.velocity.Y += deltaTime * gravity
	if rl.IsKeyPressed(rl.KeySpace) && !playerWorld.inAir {
		playerWorld.velocity.Y = jumpSpeed
	}

	// handle collisions
	proposedBoundingBox := rl.BoundingBox{
		Min: rl.Vector3Add(playerWorld.boundingBox.Min, playerWorld.velocity),
		Max: rl.Vector3Add(playerWorld.boundingBox.Max, playerWorld.velocity),
	}
	playerWorld.handleCollision(playerWorld.horizontalPosition(), proposedBoundingBox, &playerWorld.velocity)

	// determine groundedness
	playerWorld.inAir = playerWorld.velocity.Y != 0

	// do the movement
	playerWorld.camera.Position = rl.Vector3Add(playerWorld.camera.Position, playerWorld.velocity)
	playerWorld.camera.Target = rl.Vector3Add(playerWorld.camera.Target, playerWorld.velocity)
	playerWorld.boundingBox.Min = rl.Vector3Add(playerWorld.boundingBox.Min, playerWorld.velocity)
	playerWorld.boundingBox.Max = rl.Vector3Add(playerWorld.boundingBox.Max, playerWorld.velocity)

	// movement affects accuracy
	if rl.Vector3Length(playerWorld.velocity) > accurateMovementSpeedThreshold {
		playerWorld.isAccurate = false
	} else {
		playerWorld.isAccurate = true
	}

	// gun
	if playerWorld.gunState != idle {
		return
	}

	currentGun := &playerWorld.guns.guns[playerWorld.currentGun]
	switch {
	case rl.IsMouseButtonDown(rl.MouseButtonLeft) && 0 < currentGun.ammo:
		currentGun.ammo--
		rl.PlaySound(currentGun.shootSound)
		playerWorld.sendShootMessage()
		playerWorld.gunState = shooting
		currentGun.shootAnimation.setAnimationStart()
		time.AfterFunc(time.Duration(currentGun.shootTime)*time.Millisecond, func() {
			playerWorld.gunState = idle
		})

		// recoil
		rl.CameraPitch(&playerWorld.camera, recoilPitchSequence[currentGun.ammo%len(recoilPitchSequence)], 1, 0, 0)
		rl.CameraYaw(&playerWorld.camera, recoilYawSequence[currentGun.ammo%len(recoilYawSequence)], 0)

		// knockback
		lookDirection := rl.Vector3Subtract(playerWorld.camera.Target, playerWorld.camera.Position)
		lookDirection.Y = 0
		lookDirection = rl.Vector3Scale(rl.Vector3Normalize(lookDirection), currentGun.knockback)
		playerWorld.velocity = rl.Vector3Subtract(playerWorld.velocity, lookDirection)

		// check ray collisions
		var skew rl.Vector3
		if !playerWorld.isAccurate {
			skew = inaccuracySkew
		} else {
			skew = rl.Vector3Zero()
		}
		target := rl.Vector3Add(playerWorld.camera.Target, skew)
		direction := rl.Vector3Normalize(rl.Vector3Subtract(target, playerWorld.camera.Position))
		ray := rl.Ray{Position: playerWorld.camera.Position, Direction: direction}
		playerWorld.checkRayOtherPlayersCollision(ray)
	case rl.IsKeyPressed(rl.KeyR):
		playerWorld.gunState = reload
		rl.PlaySound(currentGun.reloadSound)
		time.AfterFunc(time.Duration(currentGun.reloadTime)*time.Second, func() {
			playerWorld.gunState = idle
			currentGun.ammo = currentGun.capacity
		})
	case rl.IsKeyPressed(rl.KeyQ):
		playerWorld.gunState = swapping
		rl.PlaySound(playerWorld.swapSound)
		time.AfterFunc(time.Duration(swapTime)*time.Second, func() {
			playerWorld.gunState = idle
			playerWorld.currentGun = (playerWorld.currentGun + 1) % len(playerWorld.guns.guns)
		})
	}

	// scope
	if rl.IsMouseButtonDown(rl.MouseButtonRight) && (playerWorld.gunState == idle || playerWorld.gunState == shooting) && currentGun.hasScope {
		playerWorld.scoped = true
		playerWorld.lookSensitivity = scopeSensitivity
	} else {
		playerWorld.scoped = false
		playerWorld.lookSensitivity = lookSensitivity
	}
}

// tell the server the player shot a gun, so it can broadcast to other players to let them know and play a gunshot sound
func (playerWorld *playerWorld) sendShootMessage() {
	playerWorld.connMutex.Lock()
	if err := playerWorld.conn.WriteMessage(websocket.BinaryMessage, []byte{byte(shotMessage)}); err != nil {
		log.Println(err)
	}
	playerWorld.connMutex.Unlock()
}

// https://github.com/froopy090/fps-game/blob/master/include/Utility/Collision.h#L79
func (playerWorld *playerWorld) handleCollision(playerHorizontalPosition rl.Vector2, playerBoundingBox rl.BoundingBox, velocity *rl.Vector3) {
	// use region tree data structure to only fetch the bounding boxes near the player
	for _, blockBoundingBox := range playerWorld.localBoundingBlocks(playerHorizontalPosition) {
		if !rl.CheckCollisionBoxes(playerBoundingBox, *blockBoundingBox) {
			continue
		}

		// y axis
		if playerBoundingBox.Min.Y <= blockBoundingBox.Min.Y &&
			blockBoundingBox.Max.Y <= playerBoundingBox.Max.Y {
			oldPlayerWorldCameraPositionY := playerWorld.camera.Position.Y
			playerWorld.camera.Position.Y = blockBoundingBox.Min.Y + cameraHeight
			playerWorld.camera.Target.Y += playerWorld.camera.Position.Y - oldPlayerWorldCameraPositionY
			playerWorld.boundingBox.Min.Y = blockBoundingBox.Min.Y
			playerWorld.boundingBox.Max.Y = blockBoundingBox.Min.Y + playerHeight
			velocity.Y = 0
		}

		// x z axis
		xAxisCollision := playerBoundingBox.Min.X < blockBoundingBox.Min.X || playerBoundingBox.Max.X > blockBoundingBox.Max.X
		zAxisCollision := playerBoundingBox.Min.Z < blockBoundingBox.Min.Z || playerBoundingBox.Max.Z > blockBoundingBox.Max.Z

		if xAxisCollision && zAxisCollision {
			if velocity.X > 0 && velocity.Z < 0 {
				// bottom right (lock x), top left (lock z), inside (lock both)
				if playerBoundingBox.Min.X <= blockBoundingBox.Min.X && playerBoundingBox.Min.Z < blockBoundingBox.Min.Z {
					velocity.X = 0
				} else if playerBoundingBox.Max.Z >= blockBoundingBox.Max.Z && playerBoundingBox.Max.X > blockBoundingBox.Max.X {
					velocity.Z = 0
				} else {
					velocity.X = 0
					velocity.Z = 0
				}
			} else if velocity.X < 0 && velocity.Z > 0 {
				// bottom right (lock z), top left (lock x), corner (lock both)
				if playerBoundingBox.Min.X <= blockBoundingBox.Min.X && playerBoundingBox.Min.Z < blockBoundingBox.Min.Z {
					velocity.Z = 0
				} else if playerBoundingBox.Max.Z >= blockBoundingBox.Max.Z && playerBoundingBox.Max.X > blockBoundingBox.Max.X {
					velocity.X = 0
				} else {
					velocity.X = 0
					velocity.Z = 0
				}
			} else if velocity.X < 0 && velocity.Z < 0 {
				// top right (lock z), bottom left (lock x), corner (lock both)
				if playerBoundingBox.Max.Z >= blockBoundingBox.Max.Z && playerBoundingBox.Max.X < blockBoundingBox.Max.X && playerBoundingBox.Max.X > blockBoundingBox.Min.X {
					velocity.Z = 0
				} else if playerBoundingBox.Max.X >= blockBoundingBox.Max.X && playerBoundingBox.Max.Z < blockBoundingBox.Max.Z {
					velocity.X = 0
				} else {
					velocity.X = 0
					velocity.Z = 0
				}
			} else if velocity.X > 0 && velocity.Z > 0 {
				// top right (lock x), bottom left (lock z), corner (lock both)
				if playerBoundingBox.Max.Z >= blockBoundingBox.Max.Z && playerBoundingBox.Max.X < blockBoundingBox.Max.X {
					velocity.X = 0
				} else if playerBoundingBox.Max.X >= blockBoundingBox.Max.X && playerBoundingBox.Max.Z < blockBoundingBox.Max.Z {
					velocity.Z = 0
				} else {
					velocity.X = 0
					velocity.Z = 0
				}
			}
		} else if xAxisCollision {
			velocity.X = 0
		} else if zAxisCollision {
			velocity.Z = 0
		}
	}
}

const (
	centerX            = internalWindowWidth >> 1
	centerY            = internalWindowHeight >> 1
	textXLocation      = centerX
	textYLocation      = centerY
	crosshairXLocation = textXLocation
	crosshairYLocation = textYLocation
	crossHairWidth     = 2
	crossHairLength    = 5
	scopeWidth         = 256
	scopeHeight        = 256
	halfScopeWidth     = scopeWidth >> 1
	halfScopeHeight    = scopeHeight >> 1
	scopeTopLeftX      = centerX - scopeWidth>>1
	scopeTopLeftY      = centerY - scopeHeight>>1

	leftMargin = 5
	topMargin  = 5
	lineSpace  = 15
	fontSize   = 20
)

func (playerWorld *playerWorld) drawHud() {
	// optional statistics board
	if playerWorld.statisticsBoardRequested {
		// round
		rl.DrawTextEx(playerWorld.font, fmt.Sprintf("()::%02d", playerWorld.round), rl.Vector2{X: leftMargin, Y: topMargin + (lineSpace * 2)}, fontSize, 0, rl.Black)

		// team A points
		rl.DrawTextEx(playerWorld.font, fmt.Sprintf("~A::%02d", playerWorld.teamAPoints), rl.Vector2{X: leftMargin, Y: topMargin + (lineSpace * 3)}, fontSize, 0, rl.Black)

		// team B points
		rl.DrawTextEx(playerWorld.font, fmt.Sprintf("~B::%02d", playerWorld.teamBPoints), rl.Vector2{X: leftMargin, Y: topMargin + (lineSpace * 4)}, fontSize, 0, rl.Black)

		// kill death board
		for i, otherPlayer := range playerWorld.otherPlayers {
			if playerWorld.id == i {
				rl.DrawTextEx(playerWorld.font, fmt.Sprintf("%d K:%02d D:%02d", i, playerWorld.killAmount, playerWorld.deathAmount), rl.Vector2{X: leftMargin, Y: topMargin + float32(lineSpace*(5+i))}, fontSize, 0, rl.Black)
			} else if otherPlayer.otherPlayerState != nonExistent {
				rl.DrawTextEx(playerWorld.font, fmt.Sprintf("%d K:%02d D:%02d", i, otherPlayer.killAmount, otherPlayer.deathAmount), rl.Vector2{X: leftMargin, Y: topMargin + float32(lineSpace*(5+i))}, fontSize, 0, rl.Black)
			}
		}
	}

	// no HUD in limbo mode except statistics board
	if playerWorld.playerState == limbo {
		return
	}

	currentGun := playerWorld.guns.guns[playerWorld.currentGun]

	// handle scoping
	if currentGun.hasScope && playerWorld.scoped {
		playerWorld.camera.Fovy = zoomFovy
		rl.DrawTexturePro(currentGun.scopeTexture, rl.Rectangle{X: 0, Y: 0, Width: scopeWidth, Height: scopeHeight}, rl.Rectangle{X: scopeTopLeftX, Y: scopeTopLeftY, Width: scopeWidth, Height: scopeHeight}, rl.Vector2Zero(), 0, rl.White)

		// draw cross hair lines
		rl.DrawLineEx(rl.Vector2{X: centerX, Y: centerY - halfScopeHeight}, rl.Vector2{X: centerX, Y: centerY + halfScopeHeight}, crossHairWidth, rl.Black)
		rl.DrawLineEx(rl.Vector2{X: centerX - halfScopeWidth, Y: centerY}, rl.Vector2{X: centerX + halfScopeWidth, Y: centerY}, crossHairWidth, rl.Black)

		// colour in rest of screen
		rl.DrawRectangle(0, 0, centerX-halfScopeWidth, internalWindowHeight, rl.Black)
		rl.DrawRectangle(centerX+halfScopeWidth, 0, centerX-halfScopeWidth, internalWindowHeight, rl.Black)
		return
	} else {
		playerWorld.camera.Fovy = defaultFovy
	}

	// draw gun depending on its state
	switch playerWorld.gunState {
	case idle:
		if currentGun.hasCrossHair {
			drawCrosshair()
		}
		rl.DrawTexturePro(currentGun.shootAnimation.atlas, currentGun.shootAnimation.rectangles[0], swayedGunRectangle(playerWorld.camera.Position, playerWorld.camera.Target, playerWorld.camera.Up, playerWorld.velocity, currentGun.gunRectangle), rl.Vector2Zero(), 0, rl.White)
	case shooting:
		if currentGun.hasCrossHair {
			drawCrosshair()
		}
		currentGun.shootAnimation.drawSpriteAnimationPro(swayedGunRectangle(playerWorld.camera.Position, playerWorld.camera.Target, playerWorld.camera.Up, playerWorld.velocity, currentGun.gunRectangle))
	case reload:
		rl.DrawTextEx(playerWorld.font, "RELOADING...", rl.Vector2{X: textXLocation, Y: textYLocation}, 20, 0, rl.Black)
	case swapping:
		rl.DrawTextEx(playerWorld.font, "SWAPPING...", rl.Vector2{X: textXLocation, Y: textYLocation}, 20, 0, rl.Black)
	}

	// health
	rl.DrawTextEx(playerWorld.font, fmt.Sprintf("<3::%02d", playerWorld.health), rl.Vector2{X: leftMargin, Y: topMargin + (lineSpace * 0)}, fontSize, 0, rl.Black)

	// ammo
	rl.DrawTextEx(playerWorld.font, fmt.Sprintf("==::%02d", currentGun.ammo), rl.Vector2{X: leftMargin, Y: topMargin + (lineSpace * 1)}, fontSize, 0, rl.Black)
}

func drawCrosshair() {
	rl.DrawLineEx(
		rl.Vector2{X: float32(crosshairXLocation), Y: float32(crosshairYLocation - crossHairLength)},
		rl.Vector2{X: float32(crosshairXLocation), Y: float32(crosshairYLocation + crossHairLength)},
		crossHairWidth,
		rl.Black,
	)
	rl.DrawLineEx(
		rl.Vector2{X: float32(crosshairXLocation - crossHairLength), Y: float32(crosshairYLocation)},
		rl.Vector2{X: float32(crosshairXLocation + crossHairLength), Y: float32(crosshairYLocation)},
		crossHairWidth,
		rl.Black,
	)
}

func swayedGunRectangle(position, target, up, velocity rl.Vector3, gunRectangle rl.Rectangle) rl.Rectangle {
	forward := rl.Vector3Normalize(rl.Vector3Subtract(target, position))
	right := rl.Vector3Normalize(rl.Vector3CrossProduct(forward, up))
	forwardSpeed := rl.Vector3DotProduct(velocity, forward)
	lateralSpeed := rl.Vector3DotProduct(velocity, right)
	swayedGunRectangle := gunRectangle
	swayedGunRectangle.Y -= forwardSpeed * 50
	swayedGunRectangle.X += lateralSpeed * 50
	return swayedGunRectangle
}

func (playerWorld *playerWorld) drawWorld() {
	for _, block := range playerWorld.blocks {
		rl.DrawModel(block.model, block.centrePosition, 1, rl.White)
	}
}

func (playerWorld *playerWorld) draw() {
	if playerWorld.isDamaged {
		rl.ClearBackground(rl.Red)
	} else {
		rl.ClearBackground(rl.SkyBlue)
	}
	rl.BeginMode3D(playerWorld.camera)
	playerWorld.drawWorld()
	playerWorld.drawOtherPlayers()
	rl.EndMode3D()
	playerWorld.drawHud()
}

// unload models in world
func (playerWorld *playerWorld) cleanUp() {
	for _, block := range playerWorld.blocks {
		rl.UnloadModel(block.model)
	}
}

// set the player's location
func (playerWorld *playerWorld) setPlayerLocation(location rl.Vector3) {
	playerWorld.camera.Position = rl.Vector3Add(location, rl.Vector3{X: 0, Y: cameraHeight, Z: 0})
	playerWorld.boundingBox = generatePlayerBoundingBox(location, boundingBoxHalfWidth, playerHeight)
}

// move the bounding box to the location
func updateBoundingbox(location rl.Vector3, boundingBox *rl.BoundingBox, width, height float32) {
	boundingBox.Min.X = location.X - width
	boundingBox.Min.Y = location.Y
	boundingBox.Min.Z = location.Z - width
	boundingBox.Max.X = location.X + width
	boundingBox.Max.Y = location.Y + height
	boundingBox.Max.Z = location.Z + width
}

//////// player

type playerState int

const (
	limbo playerState = iota
	normal
)

const (
	cameraHeight         = 1.5
	playerHeight         = cameraHeight + 0.5
	lookSensitivity      = 0.005
	scopeSensitivity     = lookSensitivity / 5
	defaultFovy          = 90
	zoomFovy             = 20
	boundingBoxHalfWidth = 0.35
)

var defaultPlayerPosition = rl.Vector3{X: 0, Y: cameraHeight, Z: 0}

type player struct {
	camera                                                 rl.Camera
	velocity                                               rl.Vector3
	boundingBox                                            rl.BoundingBox
	lookSensitivity                                        float32
	inAir, isAccurate, statisticsBoardRequested, isDamaged bool
	guns
	font              rl.Font
	genericShootSound rl.Sound
	hitMarkerSound    rl.Sound
	playerState
	health, killAmount, deathAmount int
}

func newPlayer(resources *resources) *player {
	return &player{
		camera: rl.Camera3D{
			Position:   defaultPlayerPosition,
			Target:     rl.Vector3{X: 1, Y: cameraHeight, Z: 0},
			Up:         rl.Vector3{X: 0, Y: 1, Z: 0},
			Fovy:       90,
			Projection: rl.CameraPerspective,
		},
		boundingBox:       generatePlayerBoundingBox(positionOffsetHeight(defaultPlayerPosition, cameraHeight), boundingBoxHalfWidth, playerHeight),
		lookSensitivity:   lookSensitivity,
		guns:              *newGuns(resources),
		font:              resources.mainFont,
		genericShootSound: resources.genericShootSound,
		hitMarkerSound:    resources.hitMarkerSound,
		health:            maxHealth,
	}
}

func (player *player) horizontalPosition() rl.Vector2 {
	return rl.Vector2{X: player.camera.Position.X, Y: player.camera.Position.Z}
}

// get position of a player at their feet
func positionOffsetHeight(position rl.Vector3, height float32) rl.Vector3 {
	return rl.Vector3{X: position.X, Y: position.Y - height, Z: position.Z}
}

func generatePlayerBoundingBox(position rl.Vector3, playerWidth, playerHeight float32) rl.BoundingBox {
	return rl.BoundingBox{
		Min: rl.Vector3{X: position.X - playerWidth, Y: position.Y, Z: position.Z - playerWidth},
		Max: rl.Vector3{X: position.X + playerWidth, Y: position.Y + playerHeight, Z: position.Z + playerWidth},
	}
}

// reset player to prepare for the round's start
func (playerWorld *playerWorld) reset() {
	playerWorld.gunState = idle
	playerWorld.guns.guns[0].ammo = playerWorld.guns.guns[0].capacity
	playerWorld.guns.guns[1].ammo = playerWorld.guns.guns[1].capacity
	playerWorld.playerState = limbo
	playerWorld.scoped = false
	playerWorld.health = maxHealth
	for i := range playerWorld.otherPlayers {
		otherPlayer := &playerWorld.otherPlayers[i]
		if otherPlayer.otherPlayerState != nonExistent {
			otherPlayer.otherPlayerState = alive
		}
	}
}

//////// world

var (
	aSpawnLocations = []rl.Vector3{
		rl.Vector3{X: -10, Y: 0, Z: 5},
		rl.Vector3{X: -10, Y: 0, Z: 0},
		rl.Vector3{X: -10, Y: 0, Z: -5},
	}
	bSpawnLocations = []rl.Vector3{
		rl.Vector3{X: 10, Y: 0, Z: 5},
		rl.Vector3{X: 10, Y: 0, Z: 0},
		rl.Vector3{X: 10, Y: 0, Z: -5},
	}
)

type world struct {
	blocks []*block
	regionTree
}

func (world *world) localBoundingBlocks(position rl.Vector2) []*rl.BoundingBox {
	for _, leaf := range world.regionTree.leaves {
		if position.X >= leaf.bottomLeft.X && position.X <= leaf.topRight.X &&
			position.Y >= leaf.bottomLeft.Y && position.Y <= leaf.topRight.Y {
			return leaf.boundingBoxes
		}
	}

	return make([]*rl.BoundingBox, 0)
}

func newWorld(resources *resources) *world {
	floorTexture := resources.textures.floorTexture
	outerWallTexture := resources.textures.outerWallTexture
	innerWallTexture := resources.textures.innerWallTexture

	floor := newFloor()
	floor.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = floorTexture

	northBarrier := newNorthOuterWall()
	northBarrier.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = outerWallTexture
	southBarrier := newSouthOuterWall()
	southBarrier.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = outerWallTexture
	eastBarrier := newEastOuterWall()
	eastBarrier.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = outerWallTexture
	westBarrier := newWestOuterWall()
	westBarrier.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = outerWallTexture

	midAWall := newMidAWall()
	midAWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botAWall := newBotAWall()
	botAWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topAWall := newTopAWall()
	topAWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botAWallComp := newBotAWallComp()
	botAWallComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topAWallComp := newTopAWallComp()
	topAWallComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botAWallSide := newBotAWallSide()
	botAWallSide.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topAWallSide := newTopAWallSide()
	topAWallSide.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botAWallSideComp := newBotAWallSideComp()
	botAWallSideComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topAWallSideComp := newTopAWallSideComp()
	topAWallSideComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	midBWall := newMidBWall()
	midBWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botBWall := newBotBWall()
	botBWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topBWall := newTopBWall()
	topBWall.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botBWallComp := newBotBWallComp()
	botBWallComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topBWallComp := newTopBWallComp()
	topBWallComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botBWallSide := newBotBWallSide()
	botBWallSide.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topBWallSide := newTopBWallSide()
	topBWallSide.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	botBWallSideComp := newBotBWallSideComp()
	botBWallSideComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	topBWallSideComp := newTopBWallSideComp()
	topBWallSideComp.model.GetMaterials()[0].GetMap(rl.MapDiffuse).Texture = innerWallTexture

	blocks := []*block{
		floor,
		northBarrier, southBarrier, eastBarrier, westBarrier,
		midAWall, botAWall, topAWall, midBWall, botBWall, topBWall,
		botAWallComp, topAWallComp, botBWallComp, topBWallComp,
		botAWallSide, topAWallSide, botBWallSide, topBWallSide,
		botAWallSideComp, topAWallSideComp, botBWallSideComp, topBWallSideComp,
	}

	regionTree := newRegionTree()
	for _, block := range blocks {
		regionTree.insertBlockIntoTree(block.boundingBox)
	}

	return &world{
		blocks:     blocks,
		regionTree: *regionTree,
	}
}

//////// block

const wallHeight = 6

type block struct {
	boundingBox    rl.BoundingBox
	model          rl.Model
	centrePosition rl.Vector3
}

func newFloor() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -11.5, Y: 0, Z: -9.5}, rl.Vector3{X: 11.5, Y: 0, Z: 9.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshPlane(23, 19, 1, 1)),
		centrePosition: rl.Vector3Zero(),
	}
}

// outer boundary walls
func newNorthOuterWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -12.5, Y: 0, Z: 9.5}, rl.Vector3{X: 12.5, Y: wallHeight, Z: 10.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(23, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 0, Y: wallHeight / 2, Z: 10},
	}
}

func newSouthOuterWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -12.5, Y: 0, Z: -10.5}, rl.Vector3{X: 12.5, Y: wallHeight, Z: -9.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(23, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 0, Y: wallHeight / 2, Z: -10},
	}
}

func newEastOuterWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -12.5, Y: 0, Z: -10.5}, rl.Vector3{X: -11.5, Y: wallHeight, Z: 10.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 19)),
		centrePosition: rl.Vector3{X: -12, Y: wallHeight / 2, Z: 0},
	}
}

func newWestOuterWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 11.5, Y: 0, Z: -10.5}, rl.Vector3{X: 12.5, Y: wallHeight, Z: 10.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 19)),
		centrePosition: rl.Vector3{X: 12, Y: wallHeight / 2, Z: 0},
	}
}

// inner walls
func newMidAWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -9.5, Y: 0, Z: -1.5}, rl.Vector3{X: -8.5, Y: wallHeight, Z: 1.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: -9, Y: wallHeight / 2, Z: 0},
	}
}

func newBotAWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -9.5, Y: 0, Z: -6.5}, rl.Vector3{X: -8.5, Y: wallHeight, Z: -3.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: -9, Y: wallHeight / 2, Z: -5},
	}
}

func newTopAWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -9.5, Y: 0, Z: 3.5}, rl.Vector3{X: -8.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: -9, Y: wallHeight / 2, Z: 5},
	}
}

func newBotAWallComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -9.5, Y: 0, Z: -6.5}, rl.Vector3{X: -6.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(2, wallHeight, 1)),
		centrePosition: rl.Vector3{X: -7.5, Y: wallHeight / 2, Z: -6},
	}
}

func newTopAWallComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -9.5, Y: 0, Z: 5.5}, rl.Vector3{X: -6.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(2, wallHeight, 1)),
		centrePosition: rl.Vector3{X: -7.5, Y: wallHeight / 2, Z: 6},
	}
}

func newBotAWallSide() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -4.5, Y: 0, Z: -6.5}, rl.Vector3{X: -1.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(3, wallHeight, 1)),
		centrePosition: rl.Vector3{X: -3, Y: wallHeight / 2, Z: -6},
	}
}

func newTopAWallSide() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -4.5, Y: 0, Z: 5.5}, rl.Vector3{X: -1.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(3, wallHeight, 1)),
		centrePosition: rl.Vector3{X: -3, Y: wallHeight / 2, Z: 6},
	}
}

func newBotAWallSideComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -2.5, Y: 0, Z: -8.5}, rl.Vector3{X: -1.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 2)),
		centrePosition: rl.Vector3{X: -2, Y: wallHeight / 2, Z: -7.5},
	}
}

func newTopAWallSideComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: -2.5, Y: 0, Z: 5.5}, rl.Vector3{X: -1.5, Y: wallHeight, Z: 8.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 2)),
		centrePosition: rl.Vector3{X: -2, Y: wallHeight / 2, Z: 7.5},
	}
}

func newMidBWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 8.5, Y: 0, Z: -1.5}, rl.Vector3{X: 9.5, Y: wallHeight, Z: 1.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: 9, Y: wallHeight / 2, Z: 0},
	}
}

func newBotBWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 8.5, Y: 0, Z: -6.5}, rl.Vector3{X: 9.5, Y: wallHeight, Z: -3.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: 9, Y: wallHeight / 2, Z: -5},
	}
}

func newTopBWall() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 8.5, Y: 0, Z: 3.5}, rl.Vector3{X: 9.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 3)),
		centrePosition: rl.Vector3{X: 9, Y: wallHeight / 2, Z: 5},
	}
}

func newBotBWallComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 6.5, Y: 0, Z: -6.5}, rl.Vector3{X: 9.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(2, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 7.5, Y: wallHeight / 2, Z: -6},
	}
}

func newTopBWallComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 6.5, Y: 0, Z: 5.5}, rl.Vector3{X: 9.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(2, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 7.5, Y: wallHeight / 2, Z: 6},
	}
}

func newBotBWallSide() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 1.5, Y: 0, Z: -6.5}, rl.Vector3{X: 4.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(3, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 3, Y: wallHeight / 2, Z: -6},
	}
}

func newTopBWallSide() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 1.5, Y: 0, Z: 5.5}, rl.Vector3{X: 4.5, Y: wallHeight, Z: 6.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(3, wallHeight, 1)),
		centrePosition: rl.Vector3{X: 3, Y: wallHeight / 2, Z: 6},
	}
}

func newBotBWallSideComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 1.5, Y: 0, Z: -8.5}, rl.Vector3{X: 2.5, Y: wallHeight, Z: -5.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 2)),
		centrePosition: rl.Vector3{X: 2, Y: wallHeight / 2, Z: -7.5},
	}
}

func newTopBWallSideComp() *block {
	return &block{
		boundingBox:    rl.NewBoundingBox(rl.Vector3{X: 1.5, Y: 0, Z: 5.5}, rl.Vector3{X: 2.5, Y: wallHeight, Z: 8.5}),
		model:          rl.LoadModelFromMesh(rl.GenMeshCube(1, wallHeight, 2)),
		centrePosition: rl.Vector3{X: 2, Y: wallHeight / 2, Z: 7.5},
	}
}

//////// region tree
//////// data structure to make sure only the regions the player is in gets
//////// checked for collisions

type regionTree struct {
	leaves []*regionTreeLeaf
}

type regionTreeLeaf struct {
	bottomLeft    rl.Vector2
	topRight      rl.Vector2
	boundingBoxes []*rl.BoundingBox
}

func newRegionTree() *regionTree {
	return &regionTree{
		leaves: []*regionTreeLeaf{
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(-11.5, 2.5),
				topRight:      rl.NewVector2(0, 9.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(0, 2.5),
				topRight:      rl.NewVector2(11.5, 9.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(-11.5, -2.5),
				topRight:      rl.NewVector2(0, 2.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(0, -2.5),
				topRight:      rl.NewVector2(11.5, 2.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(-11.5, -9.5),
				topRight:      rl.NewVector2(0, -2.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
			&regionTreeLeaf{
				bottomLeft:    rl.NewVector2(0, -9.5),
				topRight:      rl.NewVector2(11.5, -2.5),
				boundingBoxes: make([]*rl.BoundingBox, 0),
			},
		},
	}
}

// fills the region tree data structure with necessary bounding boxes in each leaf
func (regionTree *regionTree) insertBlockIntoTree(boundingBox rl.BoundingBox) {
	for _, leaf := range regionTree.leaves {
		boundingBoxBottomLeft := rl.NewVector2(boundingBox.Min.X, boundingBox.Min.Z)
		boundingBoxTopRight := rl.NewVector2(boundingBox.Max.X, boundingBox.Max.Z)
		if checkRectangleCollision(boundingBoxBottomLeft, boundingBoxTopRight, leaf.bottomLeft, leaf.topRight) {
			leaf.boundingBoxes = append(leaf.boundingBoxes, &boundingBox)
		}
	}
}

func checkRectangleCollision(bottomLeftA, topRightA, bottomLeftB, topRightB rl.Vector2) bool {
	// check if one rectangle is to the left of the other
	if topRightA.X < bottomLeftB.X || topRightB.X < bottomLeftA.X {
		return false
	}

	// check if one rectangle is above the other
	if topRightA.Y < bottomLeftB.Y || topRightB.Y < bottomLeftA.Y {
		return false
	}

	return true
}

//////// sprite animation
//////// https://www.youtube.com/watch?v=vfNn450TVxs

type spriteAnimation struct {
	atlas           rl.Texture2D
	framesPerSecond int
	rectangles      []rl.Rectangle
	latestStartTime float64
}

func newSpriteAnimation(atlas rl.Texture2D, framesPerSecond int, rectangles []rl.Rectangle) *spriteAnimation {
	return &spriteAnimation{atlas, framesPerSecond, rectangles, 0}
}

func (spriteAnimation *spriteAnimation) setAnimationStart() {
	spriteAnimation.latestStartTime = rl.GetTime()
}

func (spriteAnimation *spriteAnimation) drawSpriteAnimationPro(destinationRectangle rl.Rectangle) {
	index := int((rl.GetTime()-spriteAnimation.latestStartTime)*float64(spriteAnimation.framesPerSecond)) % len(spriteAnimation.rectangles)
	sourceRectangle := spriteAnimation.rectangles[index]
	rl.DrawTexturePro(spriteAnimation.atlas, sourceRectangle, destinationRectangle, rl.Vector2Zero(), 0, rl.White)
}

//////// gun

type gunState int

const (
	idle gunState = iota
	shooting
	reload
	swapping
)

type guns struct {
	guns       [2]gun
	currentGun int
	gunState
	scoped    bool
	swapSound rl.Sound
}

func newGuns(resources *resources) *guns {
	return &guns{
		guns: [2]gun{
			*newHandgun(resources),
			*newSniper(resources),
		},
		swapSound: resources.swapSound,
	}
}

var (
	recoilPitchSequence = [3]float32{0.05, 0.04, 0.06}
	recoilYawSequence   = [3]float32{0.02, -0.01, -0.015}
)

type gun struct {
	capacity, ammo, reloadTime, damage, shootTime int
	knockback                                     float32
	shootAnimation                                spriteAnimation
	gunRectangle                                  rl.Rectangle
	hasScope                                      bool
	hasCrossHair                                  bool
	scopeTexture                                  rl.Texture2D
	shootSound                                    rl.Sound
	reloadSound                                   rl.Sound
}

func newHandgun(resources *resources) *gun {
	return &gun{
		capacity:   30,
		ammo:       30,
		reloadTime: 3,
		damage:     1,
		shootTime:  190,
		knockback:  0.05,
		shootAnimation: *newSpriteAnimation(resources.handgunShoot, 24, []rl.Rectangle{
			rl.Rectangle{X: 0, Y: 0, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 128, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 256, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 384, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 512, Width: 128, Height: 128},
		}),
		gunRectangle: rl.Rectangle{X: internalWindowWidth>>1 - 48, Y: internalWindowHeight>>1 - 8, Width: 128, Height: 128},
		hasCrossHair: true,
		shootSound:   resources.handgunShootSound,
		reloadSound:  resources.handgunReloadSound,
	}
}

func newSniper(resources *resources) *gun {
	return &gun{
		capacity:   1,
		ammo:       1,
		reloadTime: 1,
		damage:     3,
		shootTime:  380,
		knockback:  0.25,
		shootAnimation: *newSpriteAnimation(resources.sniperShoot, 12, []rl.Rectangle{
			rl.Rectangle{X: 0, Y: 0, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 128, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 256, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 384, Width: 128, Height: 128},
			rl.Rectangle{X: 0, Y: 512, Width: 128, Height: 128},
		}),
		gunRectangle: rl.Rectangle{X: internalWindowWidth>>1 - 64, Y: internalWindowHeight>>1 - 48, Width: 192, Height: 192},
		hasScope:     true,
		scopeTexture: resources.sniperScope,
		shootSound:   resources.sniperShootSound,
		reloadSound:  resources.sniperReloadSound,
	}
}

//////// other players

var (
	otherPlayerTextureRectangle = rl.Rectangle{X: 0, Y: 0, Width: 32, Height: 64}
	otherPlayerHeight           = playerHeight
	otherPlayerWidth            = 1
)

type otherPlayerManager struct {
	otherPlayers        [maxPlayers]otherPlayer
	otherPlayerATexture rl.Texture2D
	otherPlayerBTexture rl.Texture2D
	deadPlayerTexture   rl.Texture2D
}

type otherPlayerState int

const (
	nonExistent otherPlayerState = iota
	alive
	dead
)

type otherPlayer struct {
	killAmount, deathAmount int
	position                rl.Vector3
	boundingBox             rl.BoundingBox
	otherPlayerState
}

func newOtherPlayerManager(resources *resources) *otherPlayerManager {
	return &otherPlayerManager{
		otherPlayerATexture: resources.otherPlayerA,
		otherPlayerBTexture: resources.otherPlayerB,
		deadPlayerTexture:   resources.deadPlayerTexture,
	}
}

func (playerWorld *playerWorld) drawOtherPlayers() {
	for i, otherPlayer := range playerWorld.otherPlayers {
		if otherPlayer.otherPlayerState == nonExistent {
			continue
		}
		var otherPlayerTexture rl.Texture2D
		if otherPlayer.otherPlayerState == dead {
			otherPlayerTexture = playerWorld.deadPlayerTexture
		} else if i < maxTeamPlayers {
			otherPlayerTexture = playerWorld.otherPlayerATexture
		} else {
			otherPlayerTexture = playerWorld.otherPlayerBTexture
		}
		rl.DrawBillboardRec(playerWorld.camera, otherPlayerTexture, otherPlayerTextureRectangle, offsetOtherPlayerHeight(otherPlayer.position), rl.Vector2{X: float32(otherPlayerWidth), Y: float32(otherPlayerHeight)}, rl.White)
	}
}

func offsetOtherPlayerHeight(position rl.Vector3) rl.Vector3 {
	return rl.Vector3{X: position.X, Y: position.Y + 1, Z: position.Z}
}

// handle shooting enemy players
func (playerWorld *playerWorld) checkRayOtherPlayersCollision(ray rl.Ray) {
	var opponentTeam []otherPlayer
	var teamDependantOffset int
	switch playerWorld.team {
	case a:
		opponentTeam = playerWorld.otherPlayers[maxTeamPlayers:]
		teamDependantOffset = maxTeamPlayers
	case b:
		opponentTeam = playerWorld.otherPlayers[:maxTeamPlayers]
		teamDependantOffset = 0
	}
	for otherPlayerId, otherPlayer := range opponentTeam {
		if otherPlayer.otherPlayerState == dead || otherPlayer.otherPlayerState == nonExistent {
			continue
		}
		rayCollision := rl.GetRayCollisionBox(ray, otherPlayer.boundingBox)
		if rayCollision.Hit {
			rl.PlaySound(playerWorld.hitMarkerSound)
			playerWorld.sendHitMessage(otherPlayerId + teamDependantOffset)
		}
	}
}

// let server know the client made a hit
func (playerWorld *playerWorld) sendHitMessage(hitPlayerId int) {
	playerWorld.connMutex.Lock()
	if err := playerWorld.conn.WriteMessage(websocket.BinaryMessage, []byte{byte(hitMessage), byte(hitPlayerId), byte(playerWorld.guns.guns[playerWorld.currentGun].damage)}); err != nil {
		log.Println(err)
	}
	playerWorld.connMutex.Unlock()
}

// sets the location of an other player as well as updating their bounding box accordingly
func (otherPlayer *otherPlayer) setOtherPlayerLocation(location rl.Vector3) {
	otherPlayer.position = location
	updateBoundingbox(location, &otherPlayer.boundingBox, boundingBoxHalfWidth, float32(otherPlayerHeight))
}

//////// networking

type team int

const (
	a team = iota
	b
)

type successResponse int

const (
	success successResponse = iota
	failure
)

type messageHeaders byte

const (
	nextRoundHeader messageHeaders = iota
	playHeader
	locationHeader
	shotHeader
	killedHeader
	teamPointHeader
	loseHealthHeader
	playerDisconnectHeader
)

type clientMessage byte

const (
	hitMessage clientMessage = iota
	shotMessage
	locationMessage
)

const (
	maxPlayers     = 6
	maxTeamPlayers = 6 >> 1
)

type meta struct {
	id int
	team
	conn                     *websocket.Conn
	connMutex                sync.Mutex
	round                    int
	teamAPoints, teamBPoints int
}

func newMeta(id int) *meta {
	var team team
	if id < maxTeamPlayers {
		team = a
	} else {
		team = b
	}
	return &meta{id: id, team: team}
}

func (meta *meta) connectToServer(url string) error {
	// connect to server
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return err
	}

	// send ID to the server
	idMessage := []byte{byte(meta.id)}
	if err = conn.WriteMessage(websocket.BinaryMessage, idMessage); err != nil {
		conn.Close()
		return err
	}

	// get message and check if our connection succeeded
	_, responseMessage, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return err
	}

	if len(responseMessage) != 1 || responseMessage[0] != byte(success) {
		conn.Close()
		return err
	}

	meta.conn = conn
	return nil
}

// blocks until game has started
func (playerWorld *playerWorld) waitUntilGameStarts() {
	for {
		if playerWorld.round > 0 {
			break
		}
		time.Sleep(time.Second)
	}
}

const lastRound = 10 // TODO put in common internal shared file

// prepare the start of the round
func (playerWorld *playerWorld) handleNextRound() {
	// handle ending condition
	if playerWorld.round == lastRound {
		playerWorld.exitRequested = true
		return
	}

	// set player position to the calculated spawn locations
	var location rl.Vector3
	switch playerWorld.team {
	case a:
		location = aSpawnLocations[(playerWorld.round+playerWorld.id)%len(aSpawnLocations)]
	case b:
		location = bSpawnLocations[(playerWorld.round+playerWorld.id)%len(bSpawnLocations)]
	}
	playerWorld.setPlayerLocation(location)

	// reset player attributes
	playerWorld.reset()

	playerWorld.round++

	// wait for play message before the player may continue
}

// how much the int8s are scaled from their float32 counterpart in location
// data to save packet space
const scalingFactor = 8

// receive messages from server and respond accordingly
func (playerWorld *playerWorld) receiveMessages(context context.Context) {
	for {
		select {
		case <-context.Done():
			return
		default:
			_, message, err := playerWorld.conn.ReadMessage()
			if err != nil {
				log.Println(err)
				continue
			}

			// in case of gaps in messages
			if len(message) == 0 {
				continue
			}

			switch message[0] {
			case byte(nextRoundHeader):
				playerWorld.handleNextRound()

			case byte(playHeader):
				playerWorld.playerState = normal

			case byte(locationHeader):
				// update other players accordingly
				for i := 1; i < len(message); i += 4 { // 4 is the size of each location parcel
					id := int(message[i+0])
					if id == playerWorld.id {
						continue
					}
					location := rl.Vector3{X: float32(int8(message[i+1])) / scalingFactor, Y: float32(int8(message[i+2])) / scalingFactor, Z: float32(int8(message[i+3])) / scalingFactor}
					playerWorld.otherPlayers[id].setOtherPlayerLocation(location)
					if playerWorld.otherPlayers[id].otherPlayerState == nonExistent {
						playerWorld.otherPlayers[id].otherPlayerState = otherPlayerState(normal)
					}
				}

			case byte(shotHeader):
				if len(message) != 2 {
					log.Println("Erroneous server message")
					break
				}
				// do not play sound if we get the same ID; i.e. we made the shot
				if playerWorld.id == int(message[1]) {
					break
				}
				rl.PlaySound(playerWorld.genericShootSound)

			case byte(killedHeader):
				if len(message) != 3 {
					log.Println("Erroneous server message")
					break
				}

				killerId := int(message[1])
				killedId := int(message[2])

				// if it is us who is killed, set ourself to limbo
				if playerWorld.id == killedId {
					// TODO make a function/method that does this i.e. player.die()
					playerWorld.deathAmount++
					playerWorld.playerState = limbo
				} else {
					playerWorld.otherPlayers[killedId].deathAmount++
					playerWorld.otherPlayers[killedId].otherPlayerState = dead
				}

				if playerWorld.id == killerId {
					playerWorld.killAmount++
				} else {
					playerWorld.otherPlayers[killerId].killAmount++
				}

			case byte(teamPointHeader):
				if len(message) != 2 {
					log.Println("Erroneous server message")
					break
				}

				teamThatWonPoint := team(message[1])
				switch teamThatWonPoint {
				case a:
					playerWorld.teamAPoints++
				case b:
					playerWorld.teamBPoints++
				default:
					log.Println("Deformed team point message")
				}

			case byte(loseHealthHeader):
				if len(message) != 2 {
					log.Println("Erroneous server message")
					break
				}

				// handle taking damage
				damage := int(message[1])
				playerWorld.health -= damage
				if playerWorld.health < 0 {
					playerWorld.health = 0
				}
				playerWorld.isDamaged = true
				time.AfterFunc(100 * time.Millisecond, func() {
					playerWorld.isDamaged = false
				})

			case byte(playerDisconnectHeader):
				if len(message) != 2 {
					log.Println("Erroneous server message")
					break
				}

				// handle player disconnection
				disconnectedPlayerId := int(message[1])
				playerWorld.otherPlayers[disconnectedPlayerId].otherPlayerState = nonExistent

			default:
				log.Println("Erroneous message from server")
			}
		}
	}
}

const locationUpdateFrequency = 12

// constantly update the server on our location
func (playerWorld *playerWorld) sendServerLocation() {
	for playerWorld.round == 0 {
		time.Sleep(time.Second)
	}

	ticker := time.NewTicker(time.Second / locationUpdateFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			playerWorld.connMutex.Lock()
			playerWorld.conn.WriteMessage(websocket.BinaryMessage, []byte{byte(locationMessage), byte(float32ScaleToInt8(playerWorld.camera.Position.X)), byte(float32ScaleToInt8(playerWorld.camera.Position.Y - cameraHeight)), byte(float32ScaleToInt8(playerWorld.camera.Position.Z))})
			playerWorld.connMutex.Unlock()
		}
	}
}

func float32ScaleToInt8(number float32) int8 {
	return int8(number * scalingFactor)
}

func disconnect(conn *websocket.Conn) {
	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		log.Println(err)
	}
	time.Sleep(500 * time.Millisecond)
	conn.Close()
	conn = nil
}
